# Simmer job image

The image that simmer jobs run in on Kubernetes (`Server.Kube`) needs Go, the build dependencies of the Cogent Core sims (cgo: GL, X11, Wayland), and Vulkan with the NVIDIA driver. [install.sh](install.sh) installs these on Debian bookworm, as root. It was checked on an H200 node of the Astera cluster: `vulkaninfo` lists the NVIDIA H200 as a discrete GPU, and the axon ra25 sim builds and runs on it.

The NVIDIA container runtime mounts the driver libraries and the Vulkan ICD file from the node when `NVIDIA_DRIVER_CAPABILITIES` includes graphics (simmer sets it to `all`).

## Making the image in the cluster (no docker)

[kaniko.yaml](kaniko.yaml) is a Job that builds the Dockerfile with [kaniko](https://github.com/GoogleContainerTools/kaniko) in the cluster, and pushes it with the Harbor robot secret of the namespace (`<ns>-snapshots-robot`, which can push to the `<ns>-snapshots` project). Set `--destination` and the secret name in it, then, from this directory:

```sh
kubectl -n <ns> create configmap simmer-image-context --from-file=Dockerfile --from-file=install.sh
kubectl -n <ns> create -f kaniko.yaml
kubectl -n <ns> logs -f job/simmer-image-build -c kaniko
kubectl -n <ns> delete job simmer-image-build
```

The init container copies the context out of the ConfigMap, whose files are symlinks, which kaniko would copy as such. It takes about a minute. The image in randy is `harbor.astera.sh/randy-snapshots/simmer-go:1.27.2`.

## Pulling a private image

The `<ns>-snapshots` project is private, and the Queue workers run as the default ServiceAccount of the namespace, which has no credentials for it. Make a ServiceAccount with the robot secret as its imagePullSecret, and set it as `Server.K8s.ServiceAccount` of the project (simmer passes it to `actl queue submit --service-account`):

```sh
kubectl -n <ns> apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: simmer
imagePullSecrets:
- name: <ns>-snapshots-robot
EOF
```

## Making the image with docker

The [Dockerfile](Dockerfile) runs the same install.sh. Build and push it to a Harbor project you can push to (log in first with `docker login harbor.astera.sh`; the cluster nodes are linux/amd64):

```sh
docker buildx build --platform linux/amd64 -t harbor.astera.sh/<project>/simmer-go:1.27.2 --push .
```

## Checking it

On a GPU node (a `single` workspace pod, or a one-worker Queue Entry, with `NVIDIA_DRIVER_CAPABILITIES=all`):

```sh
vulkaninfo --summary
```

It should list the NVIDIA GPU as `PHYSICAL_DEVICE_TYPE_DISCRETE_GPU`, not only `llvmpipe` (the CPU Vulkan of mesa).
