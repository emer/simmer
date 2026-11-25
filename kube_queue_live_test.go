// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cogentcore.org/lab/goal/goalib"
	"cogentcore.org/lab/table"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKubeQueueLive submits a real two-run job, of a small pure-Go sim
// (run 1 fails with exit 3), to the actl Queue, waits for it to finish,
// and checks its status, fetch and delete. It runs only with
// SIMMER_KUBE_QUEUE set to the namespace, e.g.:
//
//	SIMMER_KUBE_QUEUE=randy go test -run TestKubeQueueLive -v -timeout 30m
//
// SIMMER_KUBE_IMAGE sets the image (default: the golang image of the
// actl catalog), SIMMER_KUBE_SA the ServiceAccount (needed for a private
// image), SIMMER_KUBE_GPUS the GPUs per run (which then checks Vulkan), and SIMMER_KUBE_DRYRUN=1 stops after a dry run of the
// submit, printing the resources.
func TestKubeQueueLive(t *testing.T) {
	ns := os.Getenv("SIMMER_KUBE_QUEUE")
	if ns == "" {
		t.Skip("SIMMER_KUBE_QUEUE is not set")
	}
	sr := kubeTestSimmer()
	pr := &sr.Project
	pr.Name = "simmertest"
	pr.Job.SubCmd = true
	pr.Job.GPUs = 0
	pr.Job.CPUsPerTask = 1
	pr.Job.Memory = 1
	pr.Job.Hours = 1
	pr.Server.K8s.Namespace = ns
	pr.Server.K8s.Image = os.Getenv("SIMMER_KUBE_IMAGE")
	if pr.Server.K8s.Image == "" {
		pr.Server.K8s.Image = "harbor.astera.sh/library/golang:1.26-bookworm-2026-11-12"
	}
	if g := os.Getenv("SIMMER_KUBE_GPUS"); g != "" {
		pr.Job.GPUs, _ = strconv.Atoi(g)
		pr.SetupScript = "vulkaninfo --summary > job.$RUN.vulkan 2>&1"
	}
	pr.Server.K8s.ServiceAccount = os.Getenv("SIMMER_KUBE_SA")
	pr.FetchFiles = "*.tsv"
	pr.Update()
	sr.DataRoot = t.TempDir()
	jid := "ore00002"
	jpath := sr.JobPath(jid)
	require.NoError(t, os.MkdirAll(filepath.Join(jpath, pr.Name), 0755))
	sr.JobsTable = table.New()
	sr.JobsTable.AddStringColumn("JobID")
	sr.JobsTable.AddStringColumn("ServerJob")
	sr.JobsTable.SetNumRows(1)
	sr.JobsTable.Column("JobID").SetString1D(jid, 0)

	t.Chdir(jpath)
	goalib.WriteFile("go.mod", "module emer/simmertest\n\ngo 1.22\n")
	goalib.WriteFile("config_job.toml", "x = 1\n")
	goalib.WriteFile(filepath.Join(pr.Name, "main.go"), `package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	run := flag.Int("run", 0, "")
	flag.Int("runs", 1, "")
	flag.Bool("nogui", false, "")
	flag.String("cfg", "", "")
	flag.Parse()
	if *run == 1 {
		fmt.Println("failing")
		os.Exit(3)
	}
	os.WriteFile(fmt.Sprintf("sim_%d_train_epoch.tsv", *run), []byte("Epoch\n1\n"), 0644)
}
`)

	if os.Getenv("SIMMER_KUBE_DRYRUN") != "" {
		var b bytes.Buffer
		sr.WriteKubeScript(&b, jid, "")
		require.NoError(t, os.WriteFile("job.sbatch", b.Bytes(), 0755))
		b.Reset()
		sr.WriteKubeQueue(&b, jid)
		require.NoError(t, os.WriteFile("actl-queue.yaml", b.Bytes(), 0644))
		kubeStart()
		out := goalrun.Output("actl", sr.kubeSubmitArgs(true), "<", "/dev/null")
		err := goalError()
		t.Log(out)
		require.NoError(t, err)
		return
	}

	r := sr.KubeJobPath(jid)
	var err error
	_, err = sr.kubeExec("rm -rf " + shq(r))
	require.NoError(t, err)
	goalib.WriteFile("job.status", "Submitted")
	entry, err := sr.kubeSubmit(jid, "")
	require.NoError(t, err)
	t.Log("entry:", entry)
	sr.JobsTable.Column("ServerJob").SetString1D(entry, 0)
	defer sr.DeleteJobsKube([]string{jid})

	stat := ""
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		stat = sr.JobStatusKube(jid)
		t.Log(time.Now().Format(time.TimeOnly), stat, "|", goalib.ReadFile("job.squeue"))
		if stat == "Done" || strings.HasPrefix(stat, "Failed") {
			break
		}
		time.Sleep(15 * time.Second)
	}
	assert.Equal(t, "Failed:1", stat)
	assert.NotEmpty(t, goalib.ReadFile("job.start"))
	assert.NotEmpty(t, goalib.ReadFile("job.end"))
	out, err := sr.kubeExec("cd " + shq(r) + " && cat job.build job.0.out job.1.out")
	require.NoError(t, err)
	t.Log(out)
	assert.Contains(t, out, "failing")
	if pr.Job.GPUs > 0 {
		out, err := sr.kubeExec("cd " + shq(r) + " && grep -E 'deviceName|deviceType' job.0.vulkan")
		require.NoError(t, err)
		t.Log(out)
		assert.Contains(t, out, "DISCRETE_GPU")
	}

	sr.FetchJobKube(jid, true)
	b, err := os.ReadFile(filepath.Join(jpath, "sim_0_train_epoch.tsv"))
	require.NoError(t, err)
	assert.Equal(t, "Epoch\n1", strings.TrimSpace(string(b)))
}
