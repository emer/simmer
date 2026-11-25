// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJobCodeDir(t *testing.T) {
	for in, want := range map[string]string{
		"cfenv":             "cfenv",
		"../conflict/cfenv": "conflict/cfenv",
		"../../a/b":         "a/b",
		"./x/../y":          "y",
	} {
		if got := jobCodeDir(in); got != want {
			t.Errorf("jobCodeDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestImportRewrites checks the conflict_ff case: an empty Package (the
// module path from go.mod), a code dir in a sibling sim, and a sibling
// sim whose name has the project's name as a prefix.
func TestImportRewrites(t *testing.T) {
	mod := t.TempDir()
	os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module github.com/ccnlab/search-consat\n\ngo 1.27\n"), 0644)
	start := filepath.Join(mod, "sims", "conflict_ff")
	base := modulePath(mod)
	if base != "github.com/ccnlab/search-consat" {
		t.Fatalf("modulePath = %q", base)
	}
	rws := importRewrites(base, mod, start, "emer/sims/conflict_ff", []string{"../conflict/cfenv", "sub"})
	if len(rws) != 2 {
		t.Fatalf("rewrites: %v", rws)
	}
	src := `import (
	conflictff "github.com/ccnlab/search-consat/sims/conflict_ff"
	"github.com/ccnlab/search-consat/sims/conflict_ff/sub"
	"github.com/ccnlab/search-consat/sims/conflict/cfenv"
	"github.com/ccnlab/search-consat/sims/conflict/other"
	"github.com/ccnlab/search-consat/sims/conflict_ffx"
)
var _ = types.AddType(&types.Type{Name: "github.com/ccnlab/search-consat/sims/conflict/cfenv.CFEnv"})
`
	want := `import (
	conflictff "emer/sims/conflict_ff"
	"emer/sims/conflict_ff/sub"
	"emer/sims/conflict_ff/conflict/cfenv"
	"github.com/ccnlab/search-consat/sims/conflict/other"
	"github.com/ccnlab/search-consat/sims/conflict_ffx"
)
var _ = types.AddType(&types.Type{Name: "emer/sims/conflict_ff/conflict/cfenv.CFEnv"})
`
	if got := rewriteImports(src, rws); got != want {
		t.Errorf("rewriteImports:\n%s\nwant:\n%s", got, want)
	}

	// the conflict project must not touch the conflict_ff sibling
	rws = importRewrites(base, mod, filepath.Join(mod, "sims", "conflict"), "emer/sims/conflict", []string{"cfenv"})
	src = `"github.com/ccnlab/search-consat/sims/conflict/cfenv" "github.com/ccnlab/search-consat/sims/conflict_ff"`
	want = `"emer/sims/conflict/cfenv" "github.com/ccnlab/search-consat/sims/conflict_ff"`
	if got := rewriteImports(src, rws); got != want {
		t.Errorf("conflict rewriteImports: %s, want %s", got, want)
	}
}
