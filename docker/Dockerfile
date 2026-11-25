# Image for simmer jobs on Kubernetes (ServerParams.Kube): Go, the build
# dependencies of the Cogent Core sims, and Vulkan with the NVIDIA driver
# (see install.sh, which is also used to make the image from an actl
# workspace pod: see README.md).
#
# Build and push:
#   docker buildx build --platform linux/amd64 -t harbor.astera.sh/<project>/simmer-go:1.27.2 --push .

FROM golang:1.27-bookworm
ARG GO_VERSION=1.27.2
COPY install.sh /tmp/install.sh
RUN GO_VERSION=${GO_VERSION} bash /tmp/install.sh && rm /tmp/install.sh

ENV NVIDIA_DRIVER_CAPABILITIES=all \
	NVIDIA_VISIBLE_DEVICES=all \
	XDG_RUNTIME_DIR=/tmp \
	CGO_ENABLED=1

WORKDIR /home/dev/workspace
