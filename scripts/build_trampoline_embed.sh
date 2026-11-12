#!/usr/bin/env bash
# Cross-compile the trampoline binary for every supported darwin arch and
# place the output at internal/trampoline/download/trampoline_darwin_<arch>.
# goreleaser's before.hooks invoke this so the matching //go:embed directive
# in the darwin-only embed_*.go files bakes the binary into the CLI.
#
# Dev loop: run this locally + rebuild the CLI to pick up a fresh trampoline
# without a release cut. Otherwise `go build` consumes the zero-byte
# placeholders committed under internal/trampoline/download/ and the CLI
# falls through to the R2 download path at runtime.
set -euo pipefail

for arch in amd64 arm64; do
  echo "Building trampoline for darwin/$arch..."
  GOOS=darwin GOARCH=$arch CGO_ENABLED=0 go build -ldflags '-s -w' \
    -o "internal/trampoline/download/trampoline_darwin_$arch" \
    ./cmd/trampoline
done
