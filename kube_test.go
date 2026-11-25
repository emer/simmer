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
	assert.Equal(t, "mysim-ore00012", sr.kubeJobName("ore00012"))
	assert.Equal(t, "simmer-oreilly-obelisk-shared", sr.kubeHelperName())
	assert.Equal(t, "my-sim-x", kubeName("_My_Sim.x_"))
	assert.Equal(t, "/mnt/obelisk-shared/simserver/mysim/Oreilly/jobs/ore00012/.src", sr.kubeSourceDir("ore00012"))
	sr.Project.Server.Server = Astera
	assert.Equal(t, "/home/dev/workspace", sr.kubeSourceDir("ore00012"))
}

func TestKubeJobYAML(t *testing.T) {
	sr := kubeTestSimmer()
	k := &sr.Project.Server.K8s
	k.ServiceAccount = "simmer"
	k.SchedulerName = "kai-scheduler"
	k.NodeSelector = map[string]string{"pool": "vp", "gpu": "true"}
	k.Tolerations = []string{"nvidia.com/gpu", "dedicated:NoExecute"}
	k.Labels = map[string]string{"kai.scheduler/queue": "obelisk-vp"}
	var b bytes.Buffer
	sr.WriteKubeJob(&b, "ore00012")
	y := b.String()
	for _, s := range []string{
		"kind: Job\n", "  name: mysim-ore00012\n  namespace: obelisk\n",
		"  completionMode: Indexed\n  completions: 2\n  parallelism: 2\n",
		"    simmer/job: \"ore00012\"\n", "        kai.scheduler/queue: \"obelisk-vp\"\n",
		"      schedulerName: kai-scheduler\n", "      serviceAccountName: simmer\n",
		"      nodeSelector:\n        gpu: \"true\"\n        pool: \"vp\"\n",
		"      - {key: nvidia.com/gpu, operator: Exists, effect: NoSchedule}\n",
		"      - {key: dedicated, operator: Exists, effect: NoExecute}\n",
		"        image: harbor.astera.sh/obelisk/simmer-go:1\n",
		"        command: [bash, /mnt/obelisk-shared/simserver/mysim/Oreilly/jobs/ore00012/.src/job.sbatch]\n",
		"          requests: {cpu: \"4\", memory: 8Gi, nvidia.com/gpu: 1}\n",
		"          limits: {memory: 8Gi, nvidia.com/gpu: 1}\n",
		"          claimName: obelisk-shared\n",
		"      " + kubePodSecurity + "\n", "        " + kubeContainerSecurity + "\n",
		"        - {name: HOME, value: /tmp}",
	} {
		assert.Contains(t, y, s)
	}
	sr.Project.Job.GPUs = 0
	b.Reset()
	sr.WriteKubeJob(&b, "ore00012")
	assert.NotContains(t, b.String(), "nvidia.com/gpu:")
}

func TestKubeHelperYAML(t *testing.T) {
	sr := kubeTestSimmer()
	var b bytes.Buffer
	sr.WriteKubeHelper(&b)
	assert.NotContains(t, b.String(), "nodeSelector")
	sr.Project.Server.Server = Astera
	b.Reset()
	sr.WriteKubeHelper(&b)
	y := b.String()
	assert.Contains(t, y, "  nodeSelector:\n    astera.org/ns-obelisk: \"true\"\n    astera.org/pool: \"vp\"\n")
	assert.Contains(t, y, "  - {key: astera.org/namespace, operator: Exists, effect: NoSchedule}\n")
	assert.Contains(t, y, "      claimName: obelisk-shared\n")
	assert.Contains(t, y, "  "+kubePodSecurity+"\n")
	assert.Contains(t, y, "    "+kubeContainerSecurity+"\n")
}

// kubeScriptFile writes the worker script of the given job for the given
// server type, with the paths of the pod replaced by those in the given
// temporary directory: mnt for the shared volume, and ws for the workspace
// of Astera, or the .src directory that simmer uploads for Kube, which the
// files of the job are copied to. It returns the script file, the results
// directory, and the environment variable of the run number.
func kubeScriptFile(t *testing.T, sr *Simmer, st ServerTypes, tmp, jid string, files map[string]string) (sfile, r, runVar string) {
	sr.Project.Server.Server = st
	mnt := filepath.Join(tmp, "mnt")
	r = filepath.Join(mnt, "simserver/mysim/Oreilly/jobs", jid)
	ws := filepath.Join(tmp, "ws")
	runVar = "ACTL_WORKER_RANK"
	if st == Kube {
		ws = filepath.Join(r, ".src")
		runVar = "JOB_COMPLETION_INDEX"
	}
	for f, c := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(ws, f)), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(ws, f), []byte(c), 0644))
	}
	var b bytes.Buffer
	sr.WriteKubeScript(&b, jid, "-epochs 2")
	script := strings.ReplaceAll(b.String(), "/mnt/obelisk-shared", mnt)
	script = strings.ReplaceAll(script, "/home/dev/workspace", ws)
	sfile = filepath.Join(tmp, "job.sbatch")
	require.NoError(t, os.WriteFile(sfile, []byte(script), 0755))
	return
}

// TestKubeScript runs the worker script of a job for both runs, with a
// small sim, for Kube and Astera, with the paths of the pod replaced by
// temporary ones, and checks the results and the status files.
func TestKubeScript(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	files := map[string]string{
		"go.mod":          "module emer/mysim\n\ngo 1.22\n",
		"config_job.toml": "x = 1\n",
		"mysim/main.go": `package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	run := flag.Int("run.run", 0, "")
	flag.Int("run.runs", 1, "")
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
`}
	for _, st := range []ServerTypes{Kube, Astera} {
		t.Run(st.String(), func(t *testing.T) {
			sr := kubeTestSimmer()
			sr.Project.Job.SubCmd = true
			tmp := t.TempDir()
			sfile, r, runVar := kubeScriptFile(t, sr, st, tmp, "ore00012", files)
			home := filepath.Join(tmp, "home") // the script links ~/.cache/go-xla
			for _, run := range []string{"0", "1"} {
				cmd := exec.Command("bash", sfile)
				cmd.Env = append(os.Environ(), runVar+"="+run, "GOFLAGS=", "GOWORK=off", "HOME="+home)
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, string(out))
			}
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
			xla, err := os.Readlink(filepath.Join(home, ".cache/go-xla"))
			assert.NoError(t, err)
			assert.Equal(t, filepath.Join(tmp, "mnt/simserver/.go/oreilly/go-xla-cache"), xla)
			assert.DirExists(t, xla)
		})
	}
}

// TestKubeScriptBuildFail checks that when the build fails, every run
// fails with build-failed, with the build error in its job.<run>.out.
func TestKubeScriptBuildFail(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	sr := kubeTestSimmer()
	tmp := t.TempDir()
	sfile, r, runVar := kubeScriptFile(t, sr, Kube, tmp, "ore00013", map[string]string{
		"go.mod":  "module emer/mysim\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() { undefinedThing() }\n",
	})
	for _, run := range []string{"0", "1"} {
		cmd := exec.Command("bash", sfile)
		cmd.Env = append(os.Environ(), runVar+"="+run, "GOFLAGS=", "GOWORK=off", "HOME="+filepath.Join(tmp, "home"))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
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

// TestRunArgs tests that the run args are passed only for more than one run.
func TestRunArgs(t *testing.T) {
	sr := kubeTestSimmer()
	assert.Equal(t, " -run.run $RUN -run.runs 1", sr.runArgs("$RUN"))
	var b bytes.Buffer
	sr.WriteKubeScript(&b, "ore00012", "-epochs 2")
	assert.Contains(t, b.String(), "-cfg config_job.toml -run.run $RUN -run.runs 1 -epochs 2 > job.$RUN.out")
	sr.Project.Job.NRuns = 1
	assert.Equal(t, "", sr.runArgs("$RUN"))
	b.Reset()
	sr.WriteKubeScript(&b, "ore00012", "-epochs 2")
	assert.Contains(t, b.String(), "-cfg config_job.toml -epochs 2 > job.$RUN.out")
}

func TestStatusCount(t *testing.T) {
	assert.Equal(t, "Running", statusCount("Running", 1, 1))
	assert.Equal(t, "Failed", statusCount("Failed", 1, 0))
	assert.Equal(t, "Running:2", statusCount("Running", 2, 3))
	assert.Equal(t, "Failed:1", statusCount("Failed", 1, 2))
}
