# Simmer: simulation running and monitoring infrastructure

<p align="center">
    <a href="https://pkg.go.dev/github.com/emer/simmer"><img src="https://img.shields.io/badge/dev-reference-007d9c?logo=go&logoColor=white&style=flat" alt="pkg.go.dev docs"></a>
    <a href="https://github.com/emer/simmer/actions"><img alt="GitHub Actions Workflow Status" src="https://img.shields.io/github/actions/workflow/status/emer/simmer/go.yml"></a>
    <a href="https://raw.githack.com/wiki/emer/simmer/coverage.html"><img alt="Test Coverage" src="https://github.com/emer/simmer/wiki/coverage.svg"></a>
    <a href="https://github.com/emer/simmer/tags"><img alt="Version" src="https://img.shields.io/github/v/tag/emer/simmer?label=version&color=blue"></a>
</p>

"Watch your sims simmer on the GPU burners.."

Simmer provides a Cogent Lab based GUI for running simulation jobs, and managing and analyzing the resulting data, using the plotcore Editor. It supports both slurm and [baremetal](baremetal) job management systems.

The terminal at the bottom runs the interactive `goal` shell, in the app, with the data and the Simmer itself (as `lab.LabBrowser`), so you can work with them directly there.

# Projects

Simmer works on one simulation project at a time, which you open from the `Open recent` menu, or the list of recent projects that it shows when it starts without one, or with `Open`. Each project opens in its own simmer app, so you can run the app once, and open the projects of different simulations. Running `simmer` in the directory of a project opens it.

The settings of a project are in the `simmer.json` file in the directory of the simulation, which `Edit project` edits and saves. They include the `Server.SSH` user@host of the server, which is connected as `@1` in goal commands, and connected again after the network is lost.

`New project` makes a new project for the simulation in a given directory: the local data goes into paths like this:

`~/simdata/simname/user` with `jobs` and `labscripts` subdirectories

with a `simdata` symbolic link to it in the directory of the simulation, and a `simmer.json` file.

The server data goes into similar paths but starting with `~/simserver` instead.

# Server types

## Slurm

The slurm backend uses array jobs to distribute computation over many CPU nodes. See `slurm.go` for the relevant code.

## Bare metal

If not using slurm, then the [baremetal](baremetal) system manages jobs itself, maintaining a record of the jobs launched relative to the resources available. Simmer auto-launches and reconnects with baremetal, which runs in its own terminal window.

baremetal can be run on localhost to manage jobs on your own local machine, using this config:

```toml
[[Servers.Values]]
	Name = "lh"
	SSH = "localhost"
	NGPUs = 4
```

## Kubernetes

With `Server.Kube` set, jobs run on a Kubernetes cluster as [actl](https://actl.astera.sh) Queue Entries (see `kube.go`), like slurm array jobs: each job is one Entry, with one worker per run (`Job.NRuns`, with `Job.GPUs`, `Job.CPUsPerTask` and `Job.Memory` per CPU), which runs its run number (`-run $ACTL_WORKER_RANK -runs 1`). It needs `actl` set up (`actl start`), and these `Server.K8s` settings:

```json
"Server": {"Name": "astera", "Kube": true,
	"K8s": {"Namespace": "randy", "Image": "harbor.astera.sh/randy-snapshots/simmer-go:1.27.2",
		"ServiceAccount": "simmer"}}
```

* The job directory is uploaded as the source of the Entry (with the `job.sbatch` worker script and the `actl-queue.yaml` Entry config that simmer writes there). Worker 0 copies the job files there and builds the sim once for all (with the Go caches on the shared volume, output in `job.build`), and each worker runs it in the results directory of the job on the shared volume (`K8s.SharedPVC`, `<namespace>-shared` by default): `/mnt/<pvc>/simserver/<sim>/<user>/jobs/<job>`, so the results are there while it runs, and are kept after the Entry is deleted.

* Each run writes `job.<run>.start`, `job.<run>.out` (the output of the sim, or the error of a failed build) and `job.<run>.exit` (its exit status), and `Status` is from these files: `Pending`, `Running:n`, `Done`, or `Failed:n` when n runs failed. `Fetch` also gets `job.build` and the `job.<run>.out` files.

* `Status` and `Fetch` read the shared volume through a small helper pod, `simmer-<user>-<pvc>`, which mounts it (alpine, `sleep infinity`), with `kubectl exec`. Simmer creates it when it is not running. Delete it with `kubectl delete pod -n <namespace> simmer-<user>-<pvc>` when it is not needed.

* `Cancel` deletes the Entry (`actl queue delete`); `Delete` and `Archive` also delete the results on the shared volume. `Queue` shows `actl queue list` and `actl queue pools`.

* The [docker](docker) directory has the Dockerfile of the image, with Go and Vulkan, and how to build it in the cluster and give the workers access to it (`K8s.ServiceAccount`).

# Workflow

* `Jobs` shows current jobs in a Jobs tab.

* `Submit` runs a new sim job. Use `Message` for general, more stable info, and `Label` for a short unique identifier for this specific job.

* `Status` gets status of any running jobs.  Anything done running gets status of `Finalized` and is no longer updated by Status.  All job metadata is downloaded from host, _but not the Results_ output data, which is `Fetch`ed separately because it may be large and often needs to be consolidated in a particular way, because multiple runs of a job are executed in parallel.

* `Fetch` gets result `.tsv` files from server. It can be run on running or Finalized jobs.  When run on Finalized, then the status is set to `Fetched` and it is automatically skipped in any future Fetch actions.

* `Results` grabs specific result data files into a `Results` tab, from which further examination and plotting occurs. This step is necessary because there are typically multiple different types of results files, so you need to select which type you want view.

* `Plot` plots combined data across any selected files in `Results` tab, allowing you to compare them, using the `JobLabel` as a legend so each job has its own line color.

* `Diff` shows a diff browser for any two selected Jobs, or one selected job vs. the current sim working directory -- key for tracking changes made across jobs.

# TODO

* consat: plot mean function on test epoch is losing the TrainEpoch fields


