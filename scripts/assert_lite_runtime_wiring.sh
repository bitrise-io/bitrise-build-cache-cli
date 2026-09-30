#!/usr/bin/env bash
#
# Asserts the things a build needs that preboot could not hand it directly.
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

# The CLI has to be reachable by the name the generated configs use. Preboot
# installs it; if that did not survive into the build, the Gradle plugins get no
# token and Bazel cannot spawn its credential helper.
assert_cli_on_path() {
  command -v bitrise-build-cache >/dev/null \
    || fail "bitrise-build-cache is not on PATH — the generated configs name it and would find nothing"
  pass "CLI reachable on PATH at $(command -v bitrise-build-cache)"
}

# Machine-scoped, so preboot knows it, but it reaches the build only because the
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

# Lite could not resolve the benchmark phase, so the init script has to ask at
# build time. Assert the wiring is present AND that it enforces rather than just
# reports: a baseline answer has to be able to turn the cache off.
assert_gradle_benchmark_phase_wiring() {
  local init="${GRADLE_USER_HOME:-$HOME/.gradle}/init.d/bitrise-build-cache.init.gradle.kts"
  [[ -f "$init" ]] || fail "$init is missing — lite activation wrote no init script"
  grep -q '"benchmark-phase", "--tool", "gradle"' "$init" \
    || fail "the init script never asks for the benchmark phase — a baseline build would still use the cache"
  grep -q 'isEnabled = _bitriseBenchmarkPhase != "baseline"' "$init" \
    || fail "the benchmark phase is resolved but the remote cache does not follow it"
  # The CLI has to be able to answer, or the ValueSource fails open on every build.
  bitrise-build-cache benchmark-phase --tool gradle >/dev/null \
    || fail "benchmark-phase --tool gradle exited non-zero — the init script would silently cache as if no phase applied"
  pass "Gradle resolves the benchmark phase at build time and the cache follows it"
}

case "$TOOL" in
  gradle)
    assert_cli_on_path
    assert_gradle_benchmark_phase_wiring
    ;;
  xcode|react-native)
    assert_cli_on_path
    assert_derived_data_root
    ;;
  *)
    fail "unknown tool: $TOOL"
    ;;
esac
