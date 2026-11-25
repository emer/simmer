#!/bin/bash
# Installs what simmer jobs need on a Debian (bookworm) image as root:
# Go (GO_VERSION), the build dependencies of the Cogent Core sims, and
# Vulkan with the NVIDIA ICD. Used by the Dockerfile (see README.md).
set -euo pipefail
GO_VERSION=${GO_VERSION:-1.27.2}

apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
	gcc libc6-dev pkg-config libgl1-mesa-dev libegl1-mesa-dev xorg-dev \
	libwayland-dev libxkbcommon-dev wayland-protocols \
	libvulkan1 mesa-vulkan-drivers vulkan-tools \
	coreutils procps git curl ca-certificates
rm -rf /var/lib/apt/lists/*

# Go
if [ "$(go version 2>/dev/null | awk '{print $3}')" != "go${GO_VERSION}" ]; then
	rm -rf /usr/local/go
	curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
fi
ln -sf /usr/local/go/bin/go /usr/local/bin/go
ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

# the NVIDIA Vulkan driver: the library is mounted from the node by the
# NVIDIA container runtime when NVIDIA_DRIVER_CAPABILITIES has graphics,
# and on GPU nodes so is this ICD file (read-only), so it is only written
# when it is not there (as in a docker build).
icd=/etc/vulkan/icd.d/nvidia_icd.json
if [ ! -e $icd ]; then
	mkdir -p /etc/vulkan/icd.d
	echo '{"file_format_version":"1.0.0","ICD":{"library_path":"libGLX_nvidia.so.0","api_version":"1.3"}}' > $icd
fi

go version
