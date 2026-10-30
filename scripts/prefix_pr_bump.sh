#!/usr/bin/env bash
# Bump the pinned bitrise-build-cache CLI version + sha256 in the instance-manager startup-script prefix
# (bitrise-io/internal-platform-services, systems/instance-management/nomad_startup_script_prefix_build_cache.sh),
# one PR per branch: staging first, then production. Each PR is merged as the Bitrise Infrabot once its checks pass;
# Terraform applies the ConfigMap on merge and Reloader rolls the instance-manager pods.
#
# Run AFTER verify-release so a bump never points at a broken release. Retry-safe: a branch already at the tag is skipped.
#
# Required env:
#   BITRISE_GIT_TAG   — release tag (e.g. "v3.18.0"); the pin holds bare semver.
#   IPS_BOT_TOKEN     — GH PAT for Bitrise Infrabot on bitrise-io/internal-platform-services: contents:write,
#                       pull_requests:write, and a bypass actor on the staging and production branch rules.
# Optional env:
#   IPS_BUMP_BRANCHES — space-separated, default "staging production".
#   IPS_CHECKS_TIMEOUT — seconds to wait for a PR's checks, default 1500.
#   IPS_GIT_URL       — clone URL of the repo, default the GitHub URL with the token (overridden by tests).
set -euo pipefail

tag_v="${BITRISE_GIT_TAG:-}"
tag="${tag_v#v}"
if [[ -z "$tag" ]]; then
  echo "BITRISE_GIT_TAG is not set. Exiting."
  exit 1
fi
if [[ -z "${IPS_BOT_TOKEN:-}" ]]; then
  echo "IPS_BOT_TOKEN is not set. Exiting."
  exit 1
fi

REPO="bitrise-io/internal-platform-services"
PIN_FILE="systems/instance-management/nomad_startup_script_prefix_build_cache.sh"
BRANCHES="${IPS_BUMP_BRANCHES:-staging production}"
CHECKS_TIMEOUT="${IPS_CHECKS_TIMEOUT:-1500}"
GIT_URL="${IPS_GIT_URL:-https://x-access-token:${IPS_BOT_TOKEN}@github.com/${REPO}.git}"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

checksums_file="${workdir}/checksums.txt"
checksums_url="https://github.com/bitrise-io/bitrise-build-cache-cli/releases/download/${tag_v}/bitrise-build-cache_${tag}_checksums.txt"
echo "Downloading checksums from ${checksums_url}"
curl -fsSL --retry 5 --retry-delay 3 -o "$checksums_file" "$checksums_url"

linux_amd64_tarball="bitrise-build-cache_${tag}_linux_amd64.tar.gz"
darwin_arm64_tarball="bitrise-build-cache_${tag}_darwin_arm64.tar.gz"
linux_sha=$(awk -v f="$linux_amd64_tarball" '$2 == f {print $1}' "$checksums_file")
darwin_sha=$(awk -v f="$darwin_arm64_tarball" '$2 == f {print $1}' "$checksums_file")
if [[ -z "$linux_sha" || -z "$darwin_sha" ]]; then
  echo "Failed to extract sha256 for ${linux_amd64_tarball} or ${darwin_arm64_tarball} from checksums."
  cat "$checksums_file"
  exit 1
fi
echo "linux_amd64 sha256:  $linux_sha"
echo "darwin_arm64 sha256: $darwin_sha"

export GH_TOKEN="$IPS_BOT_TOKEN"

bump_branch() {
  local base="$1"
  local branch="chore/bump-bitrise-build-cache-cli-${tag}-${base}"
  local clone_dir="${workdir}/repo-${base}"

  echo "=== ${base} ==="
  git clone --depth=1 --branch "$base" "$GIT_URL" "$clone_dir"
  pushd "$clone_dir" >/dev/null
  git config user.name "Bitrise Infrabot"
  git config user.email "infra@bitrise.io"

  if [[ ! -f "$PIN_FILE" ]]; then
    echo "${PIN_FILE} does not exist on ${base}: the prefix variant is not rolled out there yet, skipping."
    popd >/dev/null
    return 0
  fi

  git checkout -b "$branch"
  sed -i -E "s|^(BITRISE_BUILD_CACHE_CLI_VERSION=\")[^\"]+(\")|\1${tag}\2|" "$PIN_FILE"
  sed -i -E "s|^(BITRISE_BUILD_CACHE_CLI_LINUX_AMD64_SHASUM=\")[^\"]+(\")|\1${linux_sha}\2|" "$PIN_FILE"
  sed -i -E "s|^(BITRISE_BUILD_CACHE_CLI_DARWIN_ARM64_SHASUM=\")[^\"]+(\")|\1${darwin_sha}\2|" "$PIN_FILE"

  grep -q "^BITRISE_BUILD_CACHE_CLI_VERSION=\"${tag}\"" "$PIN_FILE" || { echo "version bump failed"; exit 1; }
  grep -q "^BITRISE_BUILD_CACHE_CLI_LINUX_AMD64_SHASUM=\"${linux_sha}\"" "$PIN_FILE" || { echo "linux sha bump failed"; exit 1; }
  grep -q "^BITRISE_BUILD_CACHE_CLI_DARWIN_ARM64_SHASUM=\"${darwin_sha}\"" "$PIN_FILE" || { echo "darwin sha bump failed"; exit 1; }

  local actual_diff expected_diff
  actual_diff=$(git diff --numstat)
  if [[ -z "$actual_diff" ]]; then
    echo "${base} is already at ${tag}, skipping."
    popd >/dev/null
    return 0
  fi
  expected_diff=$(printf '3\t3\t%s' "$PIN_FILE")
  if [[ "$actual_diff" != "$expected_diff" ]]; then
    echo "Unexpected diff after sed bumps, refusing to commit." >&2
    echo "Expected numstat: ${expected_diff}" >&2
    echo "Actual numstat:   ${actual_diff}" >&2
    git diff >&2
    exit 1
  fi

  git add "$PIN_FILE"
  git commit -m "chore(instance-management): ACI-5596 bump bitrise-build-cache CLI to ${tag_v} (${base})

Automated bump from bitrise-build-cache-cli release ${tag_v}."
  git push -u origin "$branch"

  local pr_body
  pr_body=$(cat <<EOF
[ACI-5596](https://bitrise.atlassian.net/browse/ACI-5596)

Automated bump from bitrise-build-cache-cli release [${tag_v}](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/${tag_v}).

Updates the pin the VM startup prefix installs (\`${PIN_FILE}\`):

- \`${linux_amd64_tarball}\` sha256: \`${linux_sha}\`
- \`${darwin_arm64_tarball}\` sha256: \`${darwin_sha}\`

Terraform applies the ConfigMap on merge and Reloader rolls the instance-manager pods; VMs already booted keep their version.
EOF
)
  gh pr create --repo "$REPO" --base "$base" --head "$branch" \
    --title "[ACI-5596] Bump the Build Cache CLI pin to ${tag_v} (${base})" --body "$pr_body"

  local attempt
  for attempt in 1 2 3 4 5 6; do
    gh pr checks "$branch" --repo "$REPO" >/dev/null 2>&1 && break
    [[ "$attempt" == 6 ]] || sleep 10
  done
  timeout "$CHECKS_TIMEOUT" gh pr checks "$branch" --repo "$REPO" --watch --fail-fast --interval 20 \
    || { echo "Checks did not pass for the ${base} bump PR; leaving it open." >&2; exit 1; }
  gh pr merge "$branch" --repo "$REPO" --squash --delete-branch
  popd >/dev/null
  echo "Merged the ${base} prefix bump PR for ${tag_v}."
}

for base in $BRANCHES; do
  bump_branch "$base"
done
