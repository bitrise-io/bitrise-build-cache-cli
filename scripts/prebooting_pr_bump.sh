#!/usr/bin/env bash
# Open a PR against bitrise-io/build-prebooting-deployments to bump the
# bitrise-build-cache CLI version + sha256 in the preboot startup-script
# extensions (linux_amd64 + darwin_arm64). Enables auto-merge so the
# Bitrise Infrabot lands the change without human review (the bot is in
# the production branch-protection bypass list — ACI-5007).
#
# Run AFTER verify-release so we never ship a bump pointing at a broken
# release.
#
# Required env:
#   BITRISE_GIT_TAG        — the release tag (e.g. "v2.6.4"). Leading
#                            "v" is stripped for the in-script version
#                            string (the startup scripts hold bare semver).
#   PREBOOTING_BOT_TOKEN   — GH PAT for Bitrise Infrabot, scoped to
#                            bitrise-io/build-prebooting-deployments:
#                            contents:write, pull_requests:write,
#                            and on the branch-protection bypass list
#                            for `production`.
set -euo pipefail

tag_v="$BITRISE_GIT_TAG"
tag="${tag_v#v}"
if [[ -z "$tag" ]]; then
  echo "BITRISE_GIT_TAG is not set. Exiting."
  exit 1
fi
if [[ -z "${PREBOOTING_BOT_TOKEN:-}" ]]; then
  echo "PREBOOTING_BOT_TOKEN is not set. Exiting."
  exit 1
fi

REPO="bitrise-io/build-prebooting-deployments"
BRANCH="chore/bump-bitrise-build-cache-cli-${tag}"
BASE="production"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# 1. Fetch the just-published checksums file from the GH release.
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

# 2. Clone the deployments repo as the bot.
export GH_TOKEN="$PREBOOTING_BOT_TOKEN"
clone_dir="${workdir}/repo"
git clone --depth=1 --branch "$BASE" "https://x-access-token:${PREBOOTING_BOT_TOKEN}@github.com/${REPO}.git" "$clone_dir"

pushd "$clone_dir" >/dev/null
git config user.name "Bitrise Infrabot"
git config user.email "infra@bitrise.io"
git checkout -b "$BRANCH"

linux_script="preboot-reconciler/startup_script_extension_linux_bitvirt.sh"
macos_script="preboot-reconciler/startup_script_extension_macos_bitvirt.sh"

# 3. Bump the three constants (CLI_VERSION on both, plus the per-arch SHA).
#    Uses GNU sed (linux-docker stack). The bash literal `local` prefix
#    keeps the regex narrow so we never match unrelated assignments.
sed -i -E "s|^([[:space:]]*local BITRISE_BUILD_CACHE_CLI_VERSION=\")[^\"]+(\")|\1${tag}\2|" "$linux_script"
sed -i -E "s|^([[:space:]]*local BITRISE_BUILD_CACHE_CLI_LINUX_AMD64_SHASUM=\")[^\"]+(\")|\1${linux_sha}\2|" "$linux_script"

sed -i -E "s|^([[:space:]]*local BITRISE_BUILD_CACHE_CLI_VERSION=\")[^\"]+(\")|\1${tag}\2|" "$macos_script"
sed -i -E "s|^([[:space:]]*local BITRISE_BUILD_CACHE_CLI_DARWIN_ARM64_SHASUM=\")[^\"]+(\")|\1${darwin_sha}\2|" "$macos_script"

# Sanity-check the bumps actually landed.
grep -q "BITRISE_BUILD_CACHE_CLI_VERSION=\"${tag}\"" "$linux_script" || { echo "linux version bump failed"; exit 1; }
grep -q "BITRISE_BUILD_CACHE_CLI_LINUX_AMD64_SHASUM=\"${linux_sha}\"" "$linux_script" || { echo "linux sha bump failed"; exit 1; }
grep -q "BITRISE_BUILD_CACHE_CLI_VERSION=\"${tag}\"" "$macos_script" || { echo "macos version bump failed"; exit 1; }
grep -q "BITRISE_BUILD_CACHE_CLI_DARWIN_ARM64_SHASUM=\"${darwin_sha}\"" "$macos_script" || { echo "macos sha bump failed"; exit 1; }

if git diff --quiet; then
  echo "No changes to commit — bump already applied for ${tag}. Exiting."
  exit 0
fi

# 4. Commit, push, open PR, enable auto-merge.
git add "$linux_script" "$macos_script"
git commit -m "chore: bump bitrise-build-cache CLI to ${tag_v}

ACI-5007: automated bump from bitrise-build-cache-cli release ${tag_v}.

Updates:
- ${linux_amd64_tarball} sha256: ${linux_sha}
- ${darwin_arm64_tarball} sha256: ${darwin_sha}
"

git push -u origin "$BRANCH"

pr_body=$(cat <<EOF
Automated bump from bitrise-build-cache-cli release [${tag_v}](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/${tag_v}).

Bumps preboot startup-script extensions to pull CLI ${tag_v} from the host VM cache (origin: R2 \`build-cache-cli-releases\`).

- \`${linux_amd64_tarball}\` sha256: \`${linux_sha}\`
- \`${darwin_arm64_tarball}\` sha256: \`${darwin_sha}\`

Tracked in [ACI-5007](https://bitrise.atlassian.net/browse/ACI-5007).
EOF
)

gh pr create \
  --repo "$REPO" \
  --base "$BASE" \
  --head "$BRANCH" \
  --title "chore: bump bitrise-build-cache CLI to ${tag_v}" \
  --body "$pr_body"

gh pr merge "$BRANCH" --repo "$REPO" --auto --squash

popd >/dev/null
echo "Opened auto-merging PR to ${REPO} bumping CLI to ${tag_v}."
