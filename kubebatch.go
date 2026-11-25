// Copyright (c) 2026, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"cogentcore.org/core/base/errors"
	"cogentcore.org/lab/goal/goalib"
	"github.com/emer/simmer/baremetal"
)

// The Kube and Astera servers are reached through kubectl and actl, which
// take one to two seconds per call, so the status and fetch of many jobs
// is done in batches: one listing of the server jobs, and one exec in the
// helper pod for the files of all the jobs, instead of several calls per job.

// kubeStatusFiles are the globs of the files of a job that its status is
// computed from, and copied (except the per-run .start and .exit files),
// relative to its results directory.
var kubeStatusFiles = []string{"job.start", "job.end", "job.*.start", "job.*.exit", "job.build", "job.*.out", "job.label"}

// kubeJobsRoot returns the jobs directory of the project on the shared
// volume.
func (sr *Simmer) kubeJobsRoot() string {
	return path.Dir(sr.KubeJobPath("x"))
}

// kubeFilesScript returns a sh script that writes a tar.gz of the files of
// the given jobs that match the given globs (relative to each job
// directory), with paths jid/file, to stdout (nothing if there are none).
func kubeFilesScript(root string, jids, globs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cd %s || exit 1\nset --\nfor j in", shq(root))
	for _, j := range jids {
		b.WriteString(" " + shq(j))
	}
	b.WriteString("; do for f in")
	for _, g := range globs {
		b.WriteString(" \"$j\"/" + g)
	}
	b.WriteString("; do [ -f \"$f\" ] && set -- \"$@\" \"$f\"; done; done\n[ $# -gt 0 ] && tar czf - \"$@\"\ntrue")
	return b.String()
}

// kubeFetchFiles downloads the files of the given jobs that match the
// given globs into dir, as dir/jid/file, in one exec in the helper pod.
func (sr *Simmer) kubeFetchFiles(jids, globs []string, dir string) error {
	if len(jids) == 0 {
		return nil
	}
	tgz, err := os.CreateTemp("", "simmer-fetch-*.tgz")
	if err != nil {
		return err
	}
	tgz.Close()
	defer os.Remove(tgz.Name())
	if err := sr.kubeExecTo(kubeFilesScript(sr.kubeJobsRoot(), jids, globs), tgz.Name()); err != nil {
		return err
	}
	fi, err := os.Stat(tgz.Name())
	if err != nil || fi.Size() == 0 {
		return err
	}
	f, err := os.Open(tgz.Name())
	if err != nil {
		return err
	}
	defer f.Close()
	return untarJobs(f, dir, jids)
}

// untarJobs extracts a tar.gz of job files (jid/file) into dir, making the
// directories of the jobs first ([baremetal.Untar] does not make them).
func untarJobs(r io.Reader, dir string, jids []string) error {
	for _, jid := range jids {
		if err := os.MkdirAll(filepath.Join(dir, jid), 0750); err != nil {
			return err
		}
	}
	return baremetal.Untar(r, dir, true)
}

// kubeServerStatus returns the status line of each server job (Kube Job
// or Astera Entry), by name, from one listing.
func (sr *Simmer) kubeServerStatus() (map[string]string, error) {
	sr.kubeEnv()
	var out string
	var err error
	if sr.Project.Server.Server == Astera {
		out, err = sr.asteraActl("queue", "list")
	} else {
		var b []byte
		b, err = exec.Command("kubectl", "-n", sr.kubeNamespace(), "get", "jobs", "--no-headers", "-o", "wide").Output()
		out = string(b)
	}
	return parseServerStatus(out), err
}

// parseServerStatus parses a listing of server jobs (actl queue list or
// kubectl get jobs), with the job name first on each line, into a map
// from name to the line, with its fields separated by single spaces.
func parseServerStatus(out string) map[string]string {
	m := map[string]string{}
	for _, ln := range goalib.SplitLines(out) {
		fs := strings.Fields(ln)
		if len(fs) == 0 || fs[0] == "NAME" {
			continue
		}
		m[fs[0]] = strings.Join(fs, " ")
	}
	return m
}

// runsStatus returns the status of a job from the names of its status
// files (see [kubeStatusFiles]) and the contents of its per-run .exit
// files, as for [Simmer.kubeRunsStatus]: Pending, Running:n, Failed:n
// (once all the runs are done), or Done.
func runsStatus(files []string, exits map[string]string, nruns int) string {
	nruns = max(nruns, 1)
	nstart, nexit, nfail := 0, 0, 0
	for _, f := range files {
		switch {
		case f == "job.start" || f == "job.end":
		case strings.HasSuffix(f, ".start"):
			nstart++
		case strings.HasSuffix(f, ".exit"):
			nexit++
			if strings.TrimSpace(exits[f]) != "0" {
				nfail++
			}
		}
	}
	switch {
	case nexit >= nruns && nfail > 0:
		return statusCount("Failed", nfail, nruns)
	case nexit >= nruns:
		return "Done"
	case nstart > 0:
		return statusCount("Running", nstart-nexit, nruns)
	}
	return "Pending"
}

// kubeStatusJobs updates the status of the given Kube or Astera jobs, as
// [Simmer.JobStatus] does for each, with one listing of the server jobs
// and one exec in the helper pod for the files of all of them. Jobs that
// are done are fetched, also in one exec ([Simmer.kubeFetchJobs]).
func (sr *Simmer) kubeStatusJobs(jids []string) error {
	sr.kubeStart()
	var todo []string
	for _, jid := range jids {
		jpath := sr.JobPath(jid)
		if strings.TrimSpace(goalib.ReadFile(filepath.Join(jpath, "job.job"))) == "" {
			continue // not a submitted job
		}
		switch goalib.ReadFile(filepath.Join(jpath, "job.status")) {
		case "Finalized", "Fetched", "Canceled":
			continue
		}
		todo = append(todo, jid)
	}
	if len(todo) == 0 {
		return nil
	}
	sstats, serr := sr.kubeServerStatus()
	tmp, err := os.MkdirTemp("", "simmer-status-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := sr.kubeFetchFiles(todo, kubeStatusFiles, tmp); err != nil {
		return err
	}
	var done []string
	for _, jid := range todo {
		jpath := sr.JobPath(jid)
		if st, ok := sstats[strings.TrimSpace(goalib.ReadFile(filepath.Join(jpath, "job.job")))]; ok {
			goalib.WriteFile(filepath.Join(jpath, "job.squeue"), st)
		}
		src := filepath.Join(tmp, jid)
		ents, _ := os.ReadDir(src)
		var files []string
		exits := map[string]string{}
		for _, e := range ents {
			f := e.Name()
			files = append(files, f)
			b, _ := os.ReadFile(filepath.Join(src, f))
			switch {
			case strings.HasSuffix(f, ".exit"):
				exits[f] = string(b)
			case f != "job.start" && f != "job.end" && strings.HasSuffix(f, ".start"):
			case f == "job.start" || f == "job.end":
				goalib.WriteFile(filepath.Join(jpath, f), strings.TrimSpace(string(b)))
			default: // the outputs and the label
				errors.Log(os.WriteFile(filepath.Join(jpath, f), b, 0644))
			}
		}
		sstat := runsStatus(files, exits, sr.jobNRuns(jid))
		if sstat == "Done" {
			sstat = "Finalized"
			done = append(done, jid)
		}
		goalib.WriteFile(filepath.Join(jpath, "job.status"), sstat)
	}
	ferr := sr.kubeFetchJobs(done, false)
	for _, jid := range todo {
		sr.GetMeta(jid)
	}
	return errors.Join(serr, ferr)
}

// kubeFetchJobs downloads the results files ([Project.FetchFiles]) of the
// given Kube or Astera jobs, with the outputs of their builds and runs, in
// one exec in the helper pod, as [Simmer.FetchJobKube] does for each.
// Already Fetched jobs are skipped unless force is true.
func (sr *Simmer) kubeFetchJobs(jids []string, force bool) error {
	sr.kubeStart()
	var todo []string
	for _, jid := range jids {
		if !force && goalib.ReadFile(filepath.Join(sr.JobPath(jid), "job.status")) == "Fetched" {
			continue
		}
		todo = append(todo, jid)
	}
	if len(todo) == 0 {
		return nil
	}
	globs := strings.Fields(sr.Project.FetchFiles + " " + kubeOutputFiles)
	if err := sr.kubeFetchFiles(todo, globs, filepath.Join(sr.DataRoot, "jobs")); err != nil {
		return err
	}
	for _, jid := range todo {
		jpath := sr.JobPath(jid)
		if goalib.ReadFile(filepath.Join(jpath, "job.status")) == "Finalized" {
			goalib.WriteFile(filepath.Join(jpath, "job.status"), "Fetched")
			goalib.ReplaceInFile(filepath.Join(jpath, "dbmeta.toml"), "\"Finalized\"", "\"Fetched\"")
		}
	}
	return nil
}
