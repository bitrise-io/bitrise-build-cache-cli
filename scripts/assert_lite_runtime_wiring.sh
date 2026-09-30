#!/usr/bin/env bash
#
# Asserts the things a build needs that warmup could not hand it directly.
#
# Lite defers or drops several values activation used to publish through
# envman. Each one has a build-time substitute, and each substitute has already
# failed at least once in a way that looked like success — a cache step whose
# glob expanded to `/*/SourcePackages`, matched nothing, and reported OK. So
# every check here is a POSITIVE: the value exists AND something used it.
#
# Usage: assert_lite_runtime_wiring.sh <tool>   (tool: gradle | xcode | react-native)
set -euo pipefail

TOOL="${1:?usage: assert_lite_runtime_wiring.sh <gradle|xcode|react-native>}"

fail() { echo "❌ $*"; exit 1; }
pass() { echo "✅ $*"; }

# The CLI has to be reachable by the name the generated configs use. Warmup
# installs it; if that did not survive into the build, the Gradle plugins get no
# token and Bazel cannot spawn its credential helper.
assert_cli_on_path() {
  command -v bitrise-build-cache >/dev/null \
    || fail "bitrise-build-cache is not on PATH — the generated configs name it and would find nothing"
  pass "CLI reachable on PATH at $(command -v bitrise-build-cache)"
}

# Machine-scoped, so warmup knows it, but it reaches the build only because the
# preboot stand-in exported it. Cache steps target the SPM checkouts under it.
assert_derived_data_root() {
  local root="${BITRISE_XCODE_DERIVED_DATA_PATH:-}"
  [[ -n "$root" ]] \
    || fail "BITRISE_XCODE_DERIVED_DATA_PATH is unset — cache steps that glob under it would silently match nothing"
  [[ "$root" == "$(bitrise-build-cache xcelerate derived-data-path)" ]] \
    || fail "BITRISE_XCODE_DERIVED_DATA_PATH ($root) disagrees with the CLI's own answer"
  [[ -d "$root" ]] \
    || fail "$root does not exist — the wrapper never relocated a build into it, so the value is untested"
  # The positive half: a build actually landed there.
  compgen -G "$root/*" >/dev/null \
    || fail "$root is empty — nothing used it, so an empty glob would pass for the wrong reason"
  pass "DerivedData root exported, agreed with the CLI, and populated by a build"
}

case "$TOOL" in
  gradle)
    assert_cli_on_path
    ;;
  xcode|react-native)
    assert_cli_on_path
    assert_derived_data_root
    ;;
  *)
    fail "unknown tool: $TOOL"
    ;;
esac
