// Copyright (c) 2025, Cogent Core. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package baremetal

//go:generate core generate

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"cogentcore.org/core/base/errors"
	"cogentcore.org/core/base/fsx"
	"cogentcore.org/core/base/iox/tomlx"
	"cogentcore.org/core/base/keylist"
	"cogentcore.org/core/base/sharedfile"
	"cogentcore.org/lab/goal"
	"cogentcore.org/lab/goal/goalib"
	"cogentcore.org/lab/goal/interpreter"
)

// goalrun is needed for running goal commands.
var goalrun *goal.Goal

// BareMetal is the overall bare metal job manager.
type BareMetal struct {

	// Servers is the ordered list of server machines.
	Servers Servers `json:"-"`

	// NextID is the next job ID to assign.
	NextID int `edit:"-"`

	// Active has all the active (pending, running) jobs being managed,
	// in the order submitted.
	// The unique key is the bare metal job ID (int).
	Active Jobs

	// Done has all the completed jobs that have been run.
	// This list can be purged by time as needed.
	// The unique key is the bare metal job ID (int).
	Done Jobs

	// DataDir is the directory with the BareMetal directory of the config
	// and state files, which is ~/Library by default.
	DataDir string `json:"-" toml:"-"`

	// interp is the goal interpreter that we exclusively control.
	interp *interpreter.Interpreter `json:"-" toml:"-"`

	// ticker is the [time.Ticker] used to control the background update loop.
	ticker *time.Ticker `json:"-" toml:"-"`

	// Lock for responding to inputs.
	// everything below top-level input processing is assumed to be locked.
	sync.Mutex `json:"-" toml:"-"`
}

// Jobs is the ordered list of jobs, in order submitted.
type Jobs = keylist.List[int, *Job]

// Servers is the ordered list of servers, in order of use preference.
type Servers = keylist.List[string, *Server]

func NewBareMetal() *BareMetal {
	bm := &BareMetal{}
	return bm
}

// Init does the full initialization of the baremetal server.
func (bm *BareMetal) Init() {
	bm.config()
	bm.openState()
	bm.newInterpreter()
	bm.initServers()
}

// todo: should have this logic in fsx presumably

// dataDir returns the app data dir: [BareMetal.DataDir] if set,
// and otherwise ~/Library.
func (bm *BareMetal) dataDir() string {
	if bm.DataDir != "" {
		return bm.DataDir
	}
	usr, err := user.Current()
	if errors.Log(err) != nil {
		return "/tmp"
	}
	return filepath.Join(usr.HomeDir, "Library")
}

// ConfigFile returns the path of the config.toml file with the servers.
func (bm *BareMetal) ConfigFile() string {
	return filepath.Join(bm.dataDir(), "BareMetal", "config.toml")
}

// config loads a toml format config file from
// TheApp.DataDir()/BareMetal/config.toml to load the servers.
// Use [[Servers.Values]] header for each server.
func (bm *BareMetal) config() {
	file := bm.ConfigFile()
	os.MkdirAll(filepath.Dir(file), 0777)
	if !goalib.FileExists(file) {
		slog.Error("BareMetal config file not found: no servers will be configured", "File location:", file)
		return
	}
	errors.Log(tomlx.Open(bm, file))
	bm.updateServerIndexes()
}

// stateFile returns the path of the state.json file of the jobs.
func (bm *BareMetal) stateFile() string {
	return filepath.Join(bm.dataDir(), "BareMetal", "state.json")
}

// saveState saves the current active state to a JSON file:
// TheApp.DataDir()/BareMetal/state.json. It is written whole to a temporary
// file that is then renamed into place, with a lock file held (see
// [sharedfile.Lock]), so that the file is never partially written, as when
// the process is killed while saving. A backup copy of the prior file is
// kept in state.json~, which [BareMetal.openState] uses if needed.
func (bm *BareMetal) saveState() {
	file := bm.stateFile()
	os.MkdirAll(filepath.Dir(file), 0777)
	unlock, err := sharedfile.Lock(file + ".lck")
	if errors.Log(err) != nil {
		return // saved next time
	}
	defer unlock()
	b, err := json.MarshalIndent(bm, "", "\t")
	if errors.Log(err) != nil {
		return
	}

	// the backup is a copy, so that the state file always exists
	if goalib.FileExists(file) {
		errors.Log(fsx.CopyFile(file+"~", file, 0666))
	}
	tmp := file + ".tmp"
	if errors.Log(os.WriteFile(tmp, b, 0666)) != nil {
		return
	}
	if errors.Log(os.Rename(tmp, file)) != nil {
		os.Remove(tmp)
	}
}

// openState opens the current active state from the file made by
// [BareMetal.saveState], to restore to prior running state. If it cannot be
// read, as when it was truncated by a prior version, the backup is used.
func (bm *BareMetal) openState() {
	file := bm.stateFile()
	for _, fn := range []string{file, file + "~"} {
		if !goalib.FileExists(fn) {
			continue
		}
		b, err := os.ReadFile(fn)
		if err == nil {
			st := &BareMetal{}
			if err = json.Unmarshal(b, st); err == nil {
				bm.NextID, bm.Active, bm.Done = st.NextID, st.Active, st.Done
				break
			}
		}
		slog.Error("BareMetal: could not read the state file", "file", fn, "err", err)
	}
	bm.updateServerIndexes()
	bm.Active.UpdateIndexes()
	bm.Done.UpdateIndexes()
	bm.setServerUsedFromJobs()
}

// initServers initializes the server state, including opening SSH connections.
func (bm *BareMetal) initServers() {
	for _, sv := range bm.Servers.Values {
		sv.OpenSSH()
	}
	goalrun.Run("@0")
}

// updateServerIndexes updates the indexes in the Servers ordered map,
// which is needed after loading new Server config.
func (bm *BareMetal) updateServerIndexes() {
	svs := &bm.Servers
	svs.Keys = make([]string, len(svs.Values))
	for i, v := range svs.Values {
		svs.Keys[i] = v.Name
	}
	svs.UpdateIndexes()
}

// newInterpreter creates a new goal interpreter for exclusive use of bm.
func (bm *BareMetal) newInterpreter() {
	in := interpreter.NewInterpreter(interpreter.Options{})
	// has tensor, etc builtin
	in.Config()
	bm.interp = in
	goalrun = in.Goal
}

// Interactive runs the interpreter in interactive mode.
func (bm *BareMetal) Interactive() {
	bm.interp.Interactive()
}

// clearErrors clears the errors of the goal commands, which are logged
// when they happen. After a command fails, goal runs no more commands
// until they are cleared, as in a script, but the job operations of
// baremetal are independent: a command that fails, as when the network to
// a server is lost, must not stop the operations after it, such as on
// other servers, or when the network comes back.
func clearErrors() {
	goalrun.Errors = nil
}

// bgLoop is the background update loop
func (bm *BareMetal) bgLoop() {
	for {
		bm.Lock()
		if bm.ticker == nil {
			bm.Unlock()
			return
		}
		bm.Unlock()
		<-bm.ticker.C
		nrun, nfin, err := bm.UpdateJobs()
		if err != nil {
			errors.Log(err)
		} else {
			if nrun > 0 || nfin > 0 {
				slog.Info("Jobs Updated:", "N Run:", nrun, "N Finished:", nfin)
			}
		}
	}
}
