#!/usr/bin/env bash
# Post-release verification for the CLI's install pipeline.
#
# Runs four independent checks against the just-released artifacts:
#   1. GH happy path                — install via raw.githubusercontent.com,
#                                     execute CLI, assert version == tag.
#   2. GAR file content sanity      — fetch the pinned and the mutable
#                                     `latest-pointer` files from GAR
#                                     anonymously, diff against the local
#                                     installer.sh, verify VERSION == tag.
#   3. Binary tarball checksums     — fetch checksums.txt from both GH and
#                                     GAR (must be byte-identical), then
#                                     download every platform tarball from
#                                     each source and sha256-verify against
#                                     the published checksums.
#   4. Forced-GAR install path      — block github.com and
#                                     raw.githubusercontent.com via
#                                     /etc/hosts, then run installer.sh
#                                     so tag-resolution AND binary
#                                     download both have to use the GAR
#                                     fallback. Assert the installed CLI
#                                     reports the just-released version.
#
# Required env:
#   BITRISE_GIT_TAG          — the release tag, e.g. "v2.6.4"
# Optional env:
#   SKIP_HOSTS_BLOCK=1       — skip section 3 (useful for local runs on
#                              a developer machine where editing
#                              /etc/hosts is undesirable). CI must NOT
#                              set this.
set -euo pipefail

: "${BITRISE_GIT_TAG:?BITRISE_GIT_TAG must be set to the release tag}"
TAG="${BITRISE_GIT_TAG#v}"   # bare semver, no leading v

GAR_BASE="https://artifactregistry.googleapis.com/v1/projects/ip-build-cache-prod/locations/us-central1/repositories/build-cache-cli-releases/files"
TMPDIR="$(mktemp -d)"
HOSTS_BACKUP=""

# macOS aggressively caches DNS; /etc/hosts edits don't necessarily
# apply to in-flight processes until the cache is flushed. Best-effort:
# only run on Darwin, ignore failures (these commands need sudo and
# behave differently across macOS versions).
flush_dns_cache_if_mac() {
  if [[ "$(uname -s)" == "Darwin" ]]; then
    sudo dscacheutil -flushcache 2>/dev/null || true
    sudo killall -HUP mDNSResponder 2>/dev/null || true
  fi
}

cleanup() {
  if [[ -n "$HOSTS_BACKUP" && -f "$HOSTS_BACKUP" ]]; then
    echo "[cleanup] Restoring /etc/hosts from $HOSTS_BACKUP"
    sudo cp "$HOSTS_BACKUP" /etc/hosts
    flush_dns_cache_if_mac
  fi
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

assert_version() {
  local label="$1" binary="$2"
  local actual
  actual="$("$binary" version 2>&1 | tr -d '[:space:]')"
  # Strip a leading "v" if present so the assertion is robust whether
  # the CLI emits bare semver ("2.6.4") or tag-style ("v2.6.4").
  # Today goreleaser injects bare semver via ldflags; this is defensive
  # against a future config change.
  actual="${actual#v}"
  if [[ "$actual" != "$TAG" ]]; then
    echo "FAIL [$label]: expected version '$TAG', got '$actual'" >&2
    exit 1
  fi
  echo "OK   [$label]: version '$actual'"
}

# =============================================================
# Section 1: GH happy path
# =============================================================
echo "=== Section 1: install via raw.githubusercontent.com (GH happy path) ==="
curl --retry 5 -sSfL 'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' \
  | sh -s -- -b "$TMPDIR/gh-bin"
assert_version "GH happy path" "$TMPDIR/gh-bin/bitrise-build-cache"

# =============================================================
# Section 2: GAR file content sanity
# =============================================================
echo ""
echo "=== Section 2: GAR file content checks ==="

# 2a. Immutable pinned installer.sh matches the in-tree copy.
curl --retry 5 -sSfL -o "$TMPDIR/gar-pinned.sh" \
  "${GAR_BASE}/installer.sh:${TAG}:installer.sh:download?alt=media"
if ! diff -q install/installer.sh "$TMPDIR/gar-pinned.sh" >/dev/null; then
  echo "FAIL [GAR pinned installer.sh]: differs from install/installer.sh" >&2
  diff install/installer.sh "$TMPDIR/gar-pinned.sh" || true
  exit 1
fi
echo "OK   [GAR pinned installer.sh]: matches install/installer.sh"

# 2b. Mutable latest-pointer installer.sh ALSO matches.
curl --retry 5 -sSfL -o "$TMPDIR/gar-latest.sh" \
  "${GAR_BASE}/installer.sh:latest-pointer:installer.sh:download?alt=media"
if ! diff -q install/installer.sh "$TMPDIR/gar-latest.sh" >/dev/null; then
  echo "FAIL [GAR latest-pointer installer.sh]: differs from install/installer.sh" >&2
  diff install/installer.sh "$TMPDIR/gar-latest.sh" || true
  exit 1
fi
echo "OK   [GAR latest-pointer installer.sh]: matches install/installer.sh"

# 2c. Mutable latest-pointer VERSION equals the just-released bare semver.
gar_version="$(curl --retry 5 -sSfL "${GAR_BASE}/installer.sh:latest-pointer:VERSION:download?alt=media" | tr -d '[:space:]')"
if [[ "$gar_version" != "$TAG" ]]; then
  echo "FAIL [GAR latest-pointer VERSION]: expected '$TAG', got '$gar_version'" >&2
  exit 1
fi
echo "OK   [GAR latest-pointer VERSION]: '$gar_version'"

# =============================================================
# Section 3: binary tarball checksum verification (GH and GAR)
# =============================================================
echo ""
echo "=== Section 3: binary tarball checksum verification ==="

GH_RELEASE_BASE="https://github.com/bitrise-io/bitrise-build-cache-cli/releases/download/v${TAG}"
CHECKSUMS_FILE="bitrise-build-cache_${TAG}_checksums.txt"

# 3a. Fetch checksums.txt from both sources.
curl --retry 5 -sSfL -o "$TMPDIR/checksums-gh.txt" \
  "${GH_RELEASE_BASE}/${CHECKSUMS_FILE}"
curl --retry 5 -sSfL -o "$TMPDIR/checksums-gar.txt" \
  "${GAR_BASE}/bitrise-build-cache_checksums.txt:${TAG}:${CHECKSUMS_FILE}:download?alt=media"

# 3b. checksums.txt MUST be byte-identical across sources (it's the same
# file generated once by goreleaser and uploaded to both).
if ! diff -q "$TMPDIR/checksums-gh.txt" "$TMPDIR/checksums-gar.txt" >/dev/null; then
  echo "FAIL: checksums.txt differs between GH and GAR" >&2
  diff "$TMPDIR/checksums-gh.txt" "$TMPDIR/checksums-gar.txt" || true
  exit 1
fi
echo "OK   GH-checksums.txt == GAR-checksums.txt"

# Helper: verify a downloaded file's sha256 matches the checksums.txt entry.
verify_tarball_sha() {
  local label="$1" file="$2" checksums="$3"
  local filename expected actual
  filename="$(basename "$file")"
  expected="$(grep "  ${filename}\$" "$checksums" | awk '{print $1}')"
  if [[ -z "$expected" ]]; then
    echo "FAIL [$label]: $filename not present in checksums.txt" >&2
    return 1
  fi
  actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  if [[ "$expected" != "$actual" ]]; then
    echo "FAIL [$label]: $filename expected $expected got $actual" >&2
    return 1
  fi
  echo "OK   [$label]: $filename ($expected)"
}

# 3c. Verify each GAR-mirrored tarball. The release workflow mirrors 4
# explicit platforms (linux_386 ships on GH only).
GAR_PLATFORMS=(darwin_amd64 darwin_arm64 linux_amd64 linux_arm64)
for plat in "${GAR_PLATFORMS[@]}"; do
  filename="bitrise-build-cache_${TAG}_${plat}.tar.gz"
  package="bitrise-build-cache_${plat}.tar.gz"
  curl --retry 5 -sSfL -o "$TMPDIR/$filename" \
    "${GAR_BASE}/${package}:${TAG}:${filename}:download?alt=media"
  verify_tarball_sha "GAR " "$TMPDIR/$filename" "$TMPDIR/checksums-gar.txt"
  rm -f "$TMPDIR/$filename"
done

# 3d. Verify each GH-published tarball. GH has all 5 platforms.
GH_PLATFORMS=(darwin_amd64 darwin_arm64 linux_amd64 linux_arm64 linux_386)
for plat in "${GH_PLATFORMS[@]}"; do
  filename="bitrise-build-cache_${TAG}_${plat}.tar.gz"
  curl --retry 5 -sSfL -o "$TMPDIR/$filename" "${GH_RELEASE_BASE}/${filename}"
  verify_tarball_sha "GH  " "$TMPDIR/$filename" "$TMPDIR/checksums-gh.txt"
  rm -f "$TMPDIR/$filename"
done

# =============================================================
# Section 4: forced-GAR install (block GH via /etc/hosts)
# =============================================================
if [[ "${SKIP_HOSTS_BLOCK:-0}" == "1" ]]; then
  echo ""
  echo "=== Section 4: SKIPPED (SKIP_HOSTS_BLOCK=1) ==="
  echo ""
  echo "ALL CHECKS PASSED (Section 4 skipped)"
  exit 0
fi

echo ""
echo "=== Section 4: forced-GAR install (GH blocked via /etc/hosts) ==="

HOSTS_BACKUP="$TMPDIR/hosts.bak"
sudo cp /etc/hosts "$HOSTS_BACKUP"

# Route GH-hosts to a non-routable address so all attempts fail fast.
sudo tee -a /etc/hosts > /dev/null <<'HOSTS_EOF'
# verify_release.sh: temporarily blocking GH to force GAR fallback
0.0.0.0 github.com
0.0.0.0 raw.githubusercontent.com
0.0.0.0 codeload.github.com
0.0.0.0 objects.githubusercontent.com
HOSTS_EOF

flush_dns_cache_if_mac

# Sanity-check the block actually took effect before pretending the test
# means anything.
if curl --connect-timeout 3 -sSfL -o /dev/null https://github.com/ 2>/dev/null; then
  echo "FAIL: github.com is still reachable; /etc/hosts patch did not take effect" >&2
  exit 1
fi
echo "Confirmed: github.com is blocked."

# Run installer.sh from the in-tree copy (we want to test the *binary*
# download fallback and the tag-resolution fallback; we're not testing
# raw.githubusercontent.com's reachability here). The installer's
# `tag_to_version` will fail to reach github.com and fall back to GAR's
# latest-pointer:VERSION; its `download_and_validate` will fail to reach
# releases/download/... and fall back to the GAR binary mirror.
sh install/installer.sh -b "$TMPDIR/gar-bin" -d
assert_version "Forced-GAR install" "$TMPDIR/gar-bin/bitrise-build-cache"

echo ""
echo "ALL CHECKS PASSED"
