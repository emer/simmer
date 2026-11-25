// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cogentcore.org/lab/goal/goalib"
	"cogentcore.org/lab/table"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKubeLive tests the helper pod, status and fetch of a job on a real
// cluster, without the Queue: it writes the files that the workers of a
// two-run job would write into the results directory on the shared
// volume, through the helper pod. It runs only with SIMMER_KUBE_NS set
// to the namespace, e.g.:
//
//	SIMMER_KUBE_NS=randy go test -run TestKubeLive -v
//
// It deletes the results directory at the end, but not the helper pod.
func TestKubeLive(t *testing.T) {
	ns := os.Getenv("SIMMER_KUBE_NS")
	if ns == "" {
		t.Skip("SIMMER_KUBE_NS is not set")
	}
	sr := kubeTestSimmer()
	sr.Project.Name = "simmertest"
	sr.Project.Server.Server = Astera // for the scheduling of the helper pod
	sr.Project.Server.K8s.Namespace = ns
	sr.Project.FetchFiles = "*.tsv job.label"
	sr.Project.Update()
	sr.DataRoot = t.TempDir()
	jid := "ore00001"
	jpath := sr.JobPath(jid)
	require.NoError(t, os.MkdirAll(jpath, 0755))
	sr.JobsTable = table.New()
	sr.JobsTable.AddStringColumn("JobID")
	sr.JobsTable.AddStringColumn("ServerJob")
	sr.JobsTable.SetNumRows(1)
	sr.JobsTable.Column("JobID").SetString1D(jid, 0)

	r := sr.KubeJobPath(jid)
	defer sr.kubeExec("rm -rf " + shq(r))
	_, err := sr.kubeExec("rm -rf " + shq(r) + " && mkdir -p " + shq(r))
	require.NoError(t, err)

	t.Chdir(jpath)
	goalib.WriteFile("job.nruns", "2")
	goalib.WriteFile("job.status", "Submitted")

	assert.Equal(t, "Pending", sr.JobStatusKube(jid))

	_, err = sr.kubeExec("cd " + shq(r) + ` && date > job.start && date > job.0.start && date > job.1.start && echo 0 > job.0.exit && printf 'Epoch\n1\n' > sim_0_train_epoch.tsv && echo mylabel > job.label && echo run0 output > job.0.out`)
	require.NoError(t, err)
	assert.Equal(t, "Running:1", sr.JobStatusKube(jid))
	// the status gets the output of the runs
	assert.Equal(t, "run0 output", strings.TrimSpace(goalib.ReadFile("job.0.out")))

	_, err = sr.kubeExec("cd " + shq(r) + " && echo 0 > job.1.exit && date > job.end && printf 'Epoch\n2\n' > sim_1_train_epoch.tsv")
	require.NoError(t, err)
	assert.Equal(t, "Done", sr.JobStatusKube(jid))
	assert.NotEmpty(t, goalib.ReadFile("job.end"))

	goalib.WriteFile("job.status", "Finalized")
	sr.FetchJobKube(jid, false)
	b, err := os.ReadFile(filepath.Join(jpath, "sim_1_train_epoch.tsv"))
	require.NoError(t, err)
	assert.Equal(t, "Epoch\n2", strings.TrimSpace(string(b)))
	assert.Equal(t, "mylabel", strings.TrimSpace(goalib.ReadFile("job.label")))
	assert.Equal(t, "Fetched", goalib.ReadFile("job.status"))

	require.NoError(t, sr.DeleteJobsKube([]string{jid}))
	out, err := sr.kubeExec("ls " + shq(r) + " 2>&1; true")
	require.NoError(t, err)
	assert.Contains(t, out, "No such file")
}
