#!/usr/bin/env bash
#
# Emulates the preboot VM (build-prebooting-deployments,
# preboot-reconciler/startup_script_extension_*.sh) inside an e2e build.
#
# The point is negative: activation must not be able to see the build it is
# running inside. BUILD_SCOPED_ENVS below is stripped from the child environment
# before `activate --lite` runs, so a workflow that still caches afterwards has
# proved the deferral works.
#
# It is a denylist, not a hermetic environment, and it is deliberately not one:
# activation legitimately reads a long tail of machine-scoped variables (PATH,
# HOME, TMPDIR, GRADLE_USER_HOME, endpoint and mirror overrides, locale), and an
# allowlist would silently change which of those activation sees. Two residues
# therefore survive on purpose, and neither can be stripped away:
#   - BITRISE_SOURCE_DIR / BITRISE_DEPLOY_DIR / BITRISEIO_GIT_* keep pointing at
#     this build; nothing under test reads them, but they are present.
#   - The script never leaves the git checkout, so `git remote` still resolves a
#     repo URL. No amount of env stripping fixes that — only the assertions on
#     the generated config do, which is why they exist.
#
# The script also does the two jobs that belong to the VM rather than to the
# CLI: installing the binary somewhere the build can find it, and putting the
# wrapper dir on the build's PATH.
#
# Usage: preboot_emulate.sh <tool> [extra activate args...]
set -euo pipefail

CLI="${PREBOOT_CLI:?PREBOOT_CLI must point at the built bitrise-build-cache CLI}"
TOOL="${1:?usage: preboot_emulate.sh <tool> [args...]}"
shift

INSTALL_DIR="${PREBOOT_INSTALL_DIR:-/usr/local/bin}"
INSTALLED="${INSTALL_DIR}/bitrise-build-cache"

# Everything Bitrise injects per build. If activation can read any of it, the
# workflow is not testing preboot.
BUILD_SCOPED_ENVS=(
  BITRISE_IO
  BITRISE_BUILD_SLUG
  BITRISE_BUILD_NUMBER
  BITRISE_BUILD_URL
  BITRISE_BUILD_API_TOKEN
  BITRISE_APP_SLUG
  BITRISE_APP_TITLE
  BITRISE_TRIGGERED_WORKFLOW_ID
  BITRISE_TRIGGERED_WORKFLOW_TITLE
  BITRISE_STEP_EXECUTION_ID
  BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN
  BITRISE_BUILD_CACHE_AUTH_TOKEN
  BITRISE_BUILD_CACHE_WORKSPACE_ID
  BITRISEIO_BUILD_HUB_VM_TOKEN
  BITRISEIO_BUILD_HUB_VM_TOKEN_URL
  BITRISE_GIT_BRANCH
  BITRISE_GIT_COMMIT
  BITRISE_GIT_MESSAGE
  BITRISE_PULL_REQUEST
  GIT_REPOSITORY_URL
  GIT_CLONE_COMMIT_HASH
  BITRISEIO_GIT_REPOSITORY_OWNER
  BITRISEIO_GIT_REPOSITORY_SLUG
  BITRISEIO_GIT_BRANCH_DEST
  BITRISEIO_PULL_REQUEST_REPOSITORY_URL
  BITRISEIO_PULL_REQUEST_MERGE_BRANCH
  BITRISEIO_PULL_REQUEST_HEAD_BRANCH
  CI
)

maybe_sudo() {
  if [[ -w "$INSTALL_DIR" ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

# The real preboot downloads the CLI to a stable path on the build's PATH.
# /tmp is not one: clibin refuses transient paths, which leaves Bazel without a
# credential helper and the Gradle plugins without a binary to call.
echo "=== preboot: installing the CLI to ${INSTALLED}"
maybe_sudo mkdir -p "$INSTALL_DIR"
# Skip when it is already the same file: cp refuses, and under `set -e` that
# would abort a second emulation run on a VM that is already warmed up.
if [[ "$(cd "$(dirname "$CLI")" && pwd)/$(basename "$CLI")" != "$INSTALLED" ]]; then
  maybe_sudo cp "$CLI" "$INSTALLED"
fi
maybe_sudo chmod 0755 "$INSTALLED"

unset_args=()
for name in "${BUILD_SCOPED_ENVS[@]}"; do
  unset_args+=(-u "$name")
done

echo "=== preboot: activate ${TOOL} --lite $*"
env "${unset_args[@]}" "$INSTALLED" activate "$TOOL" --lite --no-update-check "$@"

# PATH is re-prepended on every run. Harmless — the wrapper dir simply appears
# more than once — and the alternative (reading back what envman already holds)
# is not worth the branch for a stand-in that runs once per VM in production.
#
# Standing in for /etc/paths.d (macOS) or the agent's environment (Linux):
# putting the wrapper ahead of /usr/bin is the VM's job, and --lite deliberately
# does not reach for envman to do it.
if [[ "$TOOL" == "xcode" || "$TOOL" == "react-native" ]]; then
  XCELERATE_BIN="${HOME}/.bitrise-xcelerate/bin"
  if [[ -d "$XCELERATE_BIN" ]]; then
    echo "=== preboot: putting ${XCELERATE_BIN} on PATH (VM-level stand-in)"
    envman add --key PATH --value "${XCELERATE_BIN}:${PATH}"
  fi

  # Also the VM's job, for the same reason as PATH: activation normally exports
  # this through envman, which belongs to a build. The value is machine-scoped —
  # the wrapper relocates every build's DerivedData into it — so a warmed-up VM
  # does know it, it just has no build to hand it to yet. Cache steps that target
  # the SPM checkouts under it read it by name.
  #
  # Asked of the CLI rather than written out here: internal/paths is the single
  # source of truth for on-disk locations, and a second copy would drift.
  DERIVED_DATA_PATH="$("$INSTALLED" xcelerate derived-data-path)"
  if [[ -n "$DERIVED_DATA_PATH" ]]; then
    echo "=== preboot: exporting BITRISE_XCODE_DERIVED_DATA_PATH=${DERIVED_DATA_PATH} (VM-level stand-in)"
    envman add --key BITRISE_XCODE_DERIVED_DATA_PATH --value "$DERIVED_DATA_PATH"
  fi
fi

echo "=== preboot preboot finished"
