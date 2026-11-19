// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package baremetal

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestState tests saving the state of the jobs and opening it again,
// with the backup of the prior state, which is used when the state file
// cannot be read, as when it was truncated.
func TestState(t *testing.T) {
	dir := t.TempDir()
	bm := NewBareMetal()
	bm.DataDir = dir
	bm.NextID = 2
	bm.Active.Add(1, &Job{ID: 1, Source: "first"})
	bm.saveState()
	bm.NextID = 3
	bm.Active.Add(2, &Job{ID: 2, Source: "second"})
	bm.saveState()
	file := bm.stateFile()
	for _, fn := range []string{".tmp", ".lck"} {
		assert.NoFileExists(t, file+fn)
	}

	open := func() *BareMetal {
		ob := NewBareMetal()
		ob.DataDir = dir
		ob.openState()
		return ob
	}
	ob := open()
	assert.Equal(t, 3, ob.NextID)
	require.Equal(t, 2, ob.Active.Len())
	assert.Equal(t, "second", ob.Active.At(2).Source)

	// a truncated state file: the backup is used
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, b[:len(b)/2], 0666))
	ob = open()
	assert.Equal(t, 2, ob.NextID, "the backup")
	require.Equal(t, 1, ob.Active.Len())
	assert.Equal(t, "first", ob.Active.At(1).Source)
}

// TestRecoverJobGPU tests that the GPU of a recovered running job is in
// use, so that a pending job is not run on it.
func TestRecoverJobGPU(t *testing.T) {
	bm := NewBareMetal()
	bm.DataDir = t.TempDir()
	bm.Servers.Add("lh", &Server{Name: "lh", NGPUs: 2})
	bm.setServerUsedFromJobs()
	sv := bm.Servers.At("lh")
	_, err := bm.recoverJob(&Job{ID: 1, Status: Running, ServerName: "lh", ServerGPU: 0})
	require.NoError(t, err)
	assert.Equal(t, 1, sv.Avail())
	assert.Equal(t, 1, sv.NextGPU(), "GPU 0 is in use")
	assert.Equal(t, -1, sv.NextGPU())

	_, err = bm.recoverJob(&Job{ID: 2, Status: Completed, ServerName: "lh", ServerGPU: 1})
	require.NoError(t, err)
	sv.FreeGPU(1)
	assert.Equal(t, 1, sv.Avail(), "a finished job does not use its GPU")
}
