#!/usr/bin/env bash
# Lipo the goreleaser-produced per-arch trampoline binaries into a universal
# binary, then write a checksums file consumed by
# internal/trampoline/download. Writes everything under dist/trampoline/ so
# the R2 upload script can pick them up.
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

# Goreleaser emits per-arch binaries under dist/trampoline_<os>_<arch>/trampoline.
arm_src="$DIST_DIR/trampoline_darwin_arm64/trampoline"
amd_src="$DIST_DIR/trampoline_darwin_amd64/trampoline"

if [[ ! -f "$arm_src" || ! -f "$amd_src" ]]; then
  echo "Expected trampoline arch binaries not found at $arm_src / $amd_src." >&2
  exit 1
fi

cp "$arm_src" "$OUT_DIR/trampoline_v${tag}_darwin_arm64"
cp "$amd_src" "$OUT_DIR/trampoline_v${tag}_darwin_amd64"

lipo -create "$arm_src" "$amd_src" -output "$OUT_DIR/trampoline_v${tag}_darwin_universal"

# Ad-hoc code-sign — macOS refuses to launch an unsigned binary from the
# toolchain bundle.
codesign --sign - --force --timestamp=none "$OUT_DIR/trampoline_v${tag}_darwin_universal"
codesign --sign - --force --timestamp=none "$OUT_DIR/trampoline_v${tag}_darwin_arm64"
codesign --sign - --force --timestamp=none "$OUT_DIR/trampoline_v${tag}_darwin_amd64"

# Checksums file: shasum format, consumed by trampoline/download.
checksums="$OUT_DIR/trampoline_v${tag}_checksums.txt"
: > "$checksums"
(
  cd "$OUT_DIR"
  shasum -a 256 "trampoline_v${tag}_darwin_arm64" "trampoline_v${tag}_darwin_amd64" "trampoline_v${tag}_darwin_universal" >> "trampoline_v${tag}_checksums.txt"
)

echo "Built trampoline artefacts under $OUT_DIR:"
ls -la "$OUT_DIR"
