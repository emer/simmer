// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

// importRewrite is an import path rewrite for the job copies of the
// .go files: Old (in the original module) becomes New (in the job module).
type importRewrite struct {
	Old, New string
}

// modulePath returns the module path in the go.mod file in dir,
// or "" if there is none.
func modulePath(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	return modfile.ModulePath(b)
}

// jobCodeDir returns where code dir ed (relative to the project dir)
// goes in the job: ed without its leading "../" elements, so that a
// code dir outside the project dir (e.g., ../conflict/cfenv for a
// package shared with a sibling sim) is inside the job, at conflict/cfenv.
func jobCodeDir(ed string) string {
	ed = filepath.ToSlash(filepath.Clean(ed))
	for strings.HasPrefix(ed, "../") {
		ed = ed[3:]
	}
	return ed
}

// importRewrites returns the import path rewrites for a job of the
// project in startDir, in the module in gomodDir, whose import path is
// origBase (the Package, or the module path). The project's packages
// (origBase + its dir in the module) go to projPath, and code dirs
// outside the project dir go to projPath + [jobCodeDir].
// They are in order of decreasing Old length, so the most specific
// rewrite applies first.
func importRewrites(origBase, gomodDir, startDir, projPath string, codeDirs []string) []importRewrite {
	subDir := filepath.ToSlash(strings.TrimPrefix(startDir, gomodDir))
	rws := []importRewrite{{Old: path.Join(origBase, subDir), New: projPath}}
	for _, ed := range codeDirs {
		jd := jobCodeDir(ed)
		if jd == filepath.ToSlash(filepath.Clean(ed)) {
			continue // inside the project dir: the project rewrite covers it
		}
		rel, err := filepath.Rel(gomodDir, filepath.Join(startDir, ed))
		if err != nil || strings.HasPrefix(filepath.ToSlash(rel), "../") {
			continue // outside the module: not ours to rewrite
		}
		rws = append(rws, importRewrite{Old: path.Join(origBase, filepath.ToSlash(rel)), New: path.Join(projPath, jd)})
	}
	slices.SortStableFunc(rws, func(a, b importRewrite) int { return len(b.Old) - len(a.Old) })
	return rws
}

// isPathChar returns whether c can be part of an import path element,
// so a rewrite must not start or end next to it: .../sims/conflict is
// not in .../sims/conflict_ff. "." ends a path, as in pkg.Type in typegen.
func isPathChar(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// rewriteImports applies the rewrites to src, only where the Old path
// is a whole path (not part of a longer path element), each once.
func rewriteImports(src string, rws []importRewrite) string {
	var b strings.Builder
	for i := 0; i < len(src); {
		done := false
		for _, rw := range rws {
			if rw.Old == "" || !strings.HasPrefix(src[i:], rw.Old) {
				continue
			}
			end := i + len(rw.Old)
			if i > 0 && (isPathChar(src[i-1]) || src[i-1] == '/' || src[i-1] == '.') {
				continue
			}
			if end < len(src) && isPathChar(src[end]) {
				continue
			}
			b.WriteString(rw.New)
			i = end
			done = true
			break
		}
		if !done {
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

// rewriteImportsInFile applies [rewriteImports] to the given file.
func rewriteImportsInFile(fname string, rws []importRewrite) error {
	b, err := os.ReadFile(fname)
	if err != nil {
		return err
	}
	s := string(b)
	r := rewriteImports(s, rws)
	if r == s {
		return nil
	}
	return os.WriteFile(fname, []byte(r), 0644)
}
