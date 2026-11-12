#!/usr/bin/env bash
# Stage goreleaser-produced per-arch + universal trampoline binaries into
# dist/trampoline/ under the versioned filenames the download client expects,
# and emit a shasum-format checksums file. Pure sha256 + file-copy work, so
# this runs on the Linux release stack (goreleaser's own universal_binaries
# does the Mach-O lipo in pure Go; ad-hoc code-signing happens install-side
# per internal/xcode_app/toolchain.go:codesignTrampoline).
#
# Required env:
#   BITRISE_GIT_TAG — release tag (e.g. "v2.6.4"). Leading "v" stripped.
set -euo pipefail

DIST_DIR="${DIST_DIR:-dist}"
OUT_DIR="$DIST_DIR/trampoline"

tag="${BITRISE_GIT_TAG#v}"
if [[ -z "$tag" ]]; then
  echo "BITRISE_GIT_TAG is not set. Exiting." >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

# goreleaser emits per-arch binaries under dist/trampoline_<os>_<arch>/trampoline
# and the universal_binaries pipeline under dist/trampoline-universal_darwin_all/trampoline.
arm_src="$DIST_DIR/trampoline_darwin_arm64/trampoline"
amd_src="$DIST_DIR/trampoline_darwin_amd64/trampoline"
uni_src="$DIST_DIR/trampoline-universal_darwin_all/trampoline"

for src in "$arm_src" "$amd_src" "$uni_src"; do
  if [[ ! -f "$src" ]]; then
    echo "Expected trampoline binary not found at $src." >&2
    exit 1
  fi
done

cp "$arm_src" "$OUT_DIR/trampoline_v${tag}_darwin_arm64"
cp "$amd_src" "$OUT_DIR/trampoline_v${tag}_darwin_amd64"
cp "$uni_src" "$OUT_DIR/trampoline_v${tag}_darwin_universal"

checksums="$OUT_DIR/trampoline_v${tag}_checksums.txt"
(
  cd "$OUT_DIR"
  sha256sum "trampoline_v${tag}_darwin_arm64" "trampoline_v${tag}_darwin_amd64" "trampoline_v${tag}_darwin_universal" > "trampoline_v${tag}_checksums.txt"
)

echo "Built trampoline artefacts under $OUT_DIR:"
ls -la "$OUT_DIR"
