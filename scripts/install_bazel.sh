#!/usr/bin/env bash
set -exo pipefail

# Download Bazelisk using curl
# Define the Bazelisk version you want to install
export BAZELISK_VERSION='1.21.0'

# Determine the operating system
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Linux*)     OS_NAME=linux;;
    Darwin*)    OS_NAME=darwin;;
    *)          echo "Unsupported OS: $OS"; exit 1;;
esac

# Determine the architecture
if [ "$OS_NAME" = "darwin" ]; then
    if [ "$ARCH" = "x86_64" ]; then
        ARCH="amd64"   # Intel Mac
    elif [ "$ARCH" = "arm64" ]; then
        ARCH="arm64"   # Apple Silicon Mac
    else
        echo "Unsupported architecture: $ARCH"; exit 1
    fi
elif [ "$OS_NAME" = "linux" ]; then
    if [ "$ARCH" = "x86_64" ]; then
        ARCH="amd64"
    elif [ "$ARCH" = "aarch64" ]; then
        ARCH="arm64"   # 64-bit ARM
    else
        echo "Unsupported architecture: $ARCH"; exit 1
    fi
fi

# Download Bazelisk using curl based on the operating system and architecture
curl -fLO "https://github.com/bazelbuild/bazelisk/releases/download/v${BAZELISK_VERSION}/bazelisk-${OS_NAME}-${ARCH}"

# Rename the downloaded file to `bazel`
mv "bazelisk-${OS_NAME}-${ARCH}" bazel

# Make the binary executable
chmod +x bazel

# Move the executable to a directory in your PATH
sudo mv bazel /usr/local/bin/

# Verify the installation
bazel --version