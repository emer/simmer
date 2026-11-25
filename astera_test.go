// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAsteraEntryName(t *testing.T) {
	out := "==> uploading source\nEntry mysim-ore00012-k7x2q submitted.\n"
	assert.Equal(t, "mysim-ore00012-k7x2q", asteraEntryName(out, "mysim-ore00012"))
	assert.Equal(t, "", asteraEntryName("nothing here", "mysim-ore00012"))
}

func TestAsteraQueueYAML(t *testing.T) {
	sr := kubeTestSimmer()
	sr.Project.Server.Server = Astera
	var b bytes.Buffer
	sr.WriteAsteraQueue(&b, "ore00012")
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
	assert.Equal(t, []string{"queue", "submit", "--no-attach", "-f", "actl-queue.yaml", "-n", "obelisk", "--no-color", "--dry-run"}, sr.asteraSubmitArgs(true))
}
