// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cogentcore.org/lab/goal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kubeTestSimmer() *Simmer {
	if goalrun == nil {
		goalrun = goal.NewGoal()
	}
	sr := &Simmer{}
	pr := &sr.Project
	pr.Name = "mysim"
	pr.User = "Oreilly"
	pr.Job.Defaults()
	pr.Job.NRuns = 2
	pr.Job.CPUsPerTask = 4
	pr.Job.Memory = 2
	pr.Server.Server = Kube
	pr.Server.K8s.Namespace = "obelisk"
	pr.Server.K8s.Image = "harbor.astera.sh/obelisk/simmer-go:1"
	pr.Update()
	return sr
}

func TestKubeNames(t *testing.T) {
	sr := kubeTestSimmer()
	assert.Equal(t, "obelisk-shared", sr.kubePVC())
	assert.Equal(t, "/mnt/obelisk-shared/simserver/mysim/Oreilly/jobs/ore00012", sr.KubeJobPath("ore00012"))
	assert.Equal(t, "mysim-ore00012", sr.kubeEntryPrefix("ore00012"))
	assert.Equal(t, "simmer-oreilly-obelisk-shared", sr.kubeHelperName())
	assert.Equal(t, "my-sim-x", kubeName("_My_Sim.x_"))

	out := "==> uploading source\nEntry mysim-ore00012-k7x2q submitted.\n"
	assert.Equal(t, "mysim-ore00012-k7x2q", kubeEntryName(out, "mysim-ore00012"))
	assert.Equal(t, "", kubeEntryName("nothing here", "mysim-ore00012"))
}

func TestKubeQueueYAML(t *testing.T) {
	sr := kubeTestSimmer()
	var b bytes.Buffer
	sr.WriteKubeQueue(&b, "ore00012")
	y := b.String()
	for _, s := range []string{
		"name: mysim-ore00012\n",
		"image: harbor.astera.sh/obelisk/simmer-go:1\n",
		"replicas: 2\n", "gpu: 1\n", "cpu: \"4\"\n", "memory: 8Gi\n",
		"pvc: obelisk-shared\n", "mountPath: /mnt/obelisk-shared\n",
		"command: [bash, job.sbatch]\n",
	} {
		assert.Contains(t, y, s)
	}
	assert.NotContains(t, y, "pool:")
}

// TestKubeScript runs the worker script of a job for both runs, with a
// small sim, with the paths of the pod replaced by temporary ones, and
// checks the results and the status files.
func TestKubeScript(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	sr := kubeTestSimmer()
	sr.Project.Job.SubCmd = true
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	mnt := filepath.Join(tmp, "mnt")
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "mysim"), 0755))
	os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module emer/mysim\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(ws, "config_job.toml"), []byte("x = 1\n"), 0644)
	os.WriteFile(filepath.Join(ws, "mysim", "main.go"), []byte(`package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	run := flag.Int("run", 0, "")
	flag.Int("runs", 1, "")
	flag.Int("epochs", 1, "")
	flag.Bool("nogui", false, "")
	flag.String("cfg", "", "")
	flag.Parse()
	if *run == 1 {
		fmt.Println("failing")
		os.Exit(3)
	}
	os.WriteFile(fmt.Sprintf("mysim_%d_train_epoch.tsv", *run), []byte("Epoch\n1\n"), 0644)
}
`), 0644)

	var b bytes.Buffer
	sr.WriteKubeScript(&b, "ore00012", "-epochs 2")
	script := strings.ReplaceAll(b.String(), "/mnt/obelisk-shared", mnt)
	script = strings.ReplaceAll(script, "/home/dev/workspace", ws)
	sfile := filepath.Join(tmp, "job.sbatch")
	require.NoError(t, os.WriteFile(sfile, []byte(script), 0755))

	for _, run := range []string{"0", "1"} {
		cmd := exec.Command("bash", sfile)
		cmd.Env = append(os.Environ(), "ACTL_WORKER_RANK="+run, "GOFLAGS=", "GOWORK=off")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	r := filepath.Join(mnt, "simserver/mysim/Oreilly/jobs/ore00012")
	read := func(f string) string {
		b, _ := os.ReadFile(filepath.Join(r, f))
		return strings.TrimSpace(string(b))
	}
	assert.Equal(t, "0", read("job.0.exit"), read("job.build")+read("job.0.out"))
	assert.Equal(t, "3", read("job.1.exit"))
	assert.Equal(t, "failing", read("job.1.out"))
	assert.Equal(t, "Epoch\n1", read("mysim_0_train_epoch.tsv"))
	assert.Equal(t, "x = 1", read("config_job.toml")) // the job files are copied
	assert.NotEmpty(t, read("job.start"))
	assert.NotEmpty(t, read("job.end"))
	assert.NotEmpty(t, read("job.0.start"))
}

// TestKubeScriptBuildFail checks that when the build fails, every run
// fails with build-failed, with the build error in its job.<run>.out.
func TestKubeScriptBuildFail(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	sr := kubeTestSimmer()
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	mnt := filepath.Join(tmp, "mnt")
	require.NoError(t, os.MkdirAll(ws, 0755))
	os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module emer/mysim\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(ws, "main.go"), []byte("package main\n\nfunc main() { undefinedThing() }\n"), 0644)

	var b bytes.Buffer
	sr.WriteKubeScript(&b, "ore00013", "")
	script := strings.ReplaceAll(b.String(), "/mnt/obelisk-shared", mnt)
	script = strings.ReplaceAll(script, "/home/dev/workspace", ws)
	sfile := filepath.Join(tmp, "job.sbatch")
	require.NoError(t, os.WriteFile(sfile, []byte(script), 0755))
	for _, run := range []string{"0", "1"} {
		cmd := exec.Command("bash", sfile)
		cmd.Env = append(os.Environ(), "ACTL_WORKER_RANK="+run, "GOFLAGS=", "GOWORK=off")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	r := filepath.Join(mnt, "simserver/mysim/Oreilly/jobs/ore00013")
	read := func(f string) string {
		b, _ := os.ReadFile(filepath.Join(r, f))
		return strings.TrimSpace(string(b))
	}
	for _, run := range []string{"0", "1"} {
		assert.Equal(t, "build-failed", read("job."+run+".exit"))
		assert.Contains(t, read("job."+run+".out"), "undefined: undefinedThing")
	}
	assert.NotEmpty(t, read("job.end"))
}
