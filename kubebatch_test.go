// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunsStatus(t *testing.T) {
	ok := map[string]string{"job.0.exit": "0\n", "job.1.exit": "0"}
	fail := map[string]string{"job.0.exit": "0", "job.1.exit": "build-failed"}
	assert.Equal(t, "Pending", runsStatus([]string{"job.label"}, nil, 1))
	assert.Equal(t, "Running", runsStatus([]string{"job.start", "job.0.start"}, nil, 1))
	assert.Equal(t, "Running:1", runsStatus([]string{"job.0.start", "job.1.start", "job.0.exit"}, ok, 2))
	assert.Equal(t, "Done", runsStatus([]string{"job.0.start", "job.1.start", "job.0.exit", "job.1.exit", "job.end"}, ok, 2))
	assert.Equal(t, "Failed:1", runsStatus([]string{"job.0.start", "job.1.start", "job.0.exit", "job.1.exit"}, fail, 2))
	assert.Equal(t, "Failed", runsStatus([]string{"job.0.start", "job.0.exit"}, map[string]string{"job.0.exit": "124"}, 1))
}

func TestParseServerStatus(t *testing.T) {
	out := `NAME                           OWNER      POOL  QUEUE     STATE      REPLICAS  GPUS/REPLICA  AGE
conflict-ff-ore00040-622f1154  rcoreilly  vp    randy-vp  completed  1         1             27m38s
conflict-ff-ore00039-2cb1311a  rcoreilly  vp    randy-vp  running    1         1             27m49s
`
	m := parseServerStatus(out)
	assert.Len(t, m, 2)
	assert.Equal(t, "conflict-ff-ore00039-2cb1311a rcoreilly vp randy-vp running 1 1 27m49s", m["conflict-ff-ore00039-2cb1311a"])
}

// TestKubeFilesScript runs the batch script locally on a jobs directory,
// and checks that the tar.gz has just the matching files of the jobs.
func TestKubeFilesScript(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"ore00001/job.start": "s", "ore00001/job.0.start": "s", "ore00001/job.0.exit": "0",
		"ore00001/job.0.out": "out", "ore00001/a_train_epoch.tsv": "x", "ore00001/job.sbatch": "no",
		"ore00002/job.label": "Search 2: A=1", "ore00002/b.tsv": "y",
		"ore00003/job.start": "not asked for",
	}
	for f, c := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(root, f), []byte(c), 0644))
	}
	script := kubeFilesScript(root, []string{"ore00001", "ore00002", "ore00009"}, append(slices.Clone(kubeStatusFiles), "*.tsv"))
	out, err := exec.Command("sh", "-c", script).Output()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, untarJobs(bytes.NewReader(out), dir, []string{"ore00001", "ore00002", "ore00009"}))
	var got []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			r, _ := filepath.Rel(dir, p)
			got = append(got, filepath.ToSlash(r))
		}
		return nil
	})
	slices.Sort(got)
	assert.Equal(t, []string{"ore00001/a_train_epoch.tsv", "ore00001/job.0.exit", "ore00001/job.0.out", "ore00001/job.0.start", "ore00001/job.start", "ore00002/b.tsv", "ore00002/job.label"}, got)

	// no matching files: no output, and no error
	out, err = exec.Command("sh", "-c", kubeFilesScript(root, []string{"ore00009"}, kubeStatusFiles)).Output()
	require.NoError(t, err)
	assert.Empty(t, out)
}
