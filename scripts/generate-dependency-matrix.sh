#!/bin/bash

# Generate docs/dependency-matrix.md by:
#   1. Reading internal/consts/consts.go at each CLI release tag (no binary download, no
#      `activate gradle` invocation — so no side effects like the benchmark-phase API call
#      that previously zeroed out the cache plugin column for every CLI >= v1.1.0).
#   2. For each consumer step that bundles the CLI, walking the step's git tags and
#      reading either step.sh (bash toolkit) or go.mod (Go toolkit) to find the pinned
#      CLI version, then joining back to the per-CLI plugin versions from step 1.

set -eo pipefail

mkdir -p docs
RESULT_MD_PATH="${RESULT_MD_PATH:-docs/dependency-matrix.md}"
MD_HEADER_PATH="${MD_HEADER_PATH:-assets/dependency-matrix-header.md}"
CLI_RELEASE_URL_PREFIX="https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag"

CLI_REPO="bitrise-io/bitrise-build-cache-cli"
CONSTS_PATH="internal/consts/consts.go"

# Consumer steps that bundle the CLI. Pipe-delimited: human_name|repo
# For each step tag we try, in order: step.sh, step/cli.go, go.mod — first match wins.
# (Different steps pin the CLI in different ways: bash-toolkit steps export
# BITRISE_BUILD_CACHE_CLI_VERSION in step.sh; the RN step hard-codes cliVersion in
# step/cli.go; other Go-toolkit steps reference the CLI module from go.mod.)
STEPS=(
  "activate-build-cache-for-gradle|bitrise-step-activate-gradle-remote-cache"
  "activate-react-native-features|bitrise-step-activate-react-native-features"
  "activate-gradle-mirrors|bitrise-step-activate-gradle-mirrors"
  "activate-gradle-features|bitrise-step-activate-gradle-features"
)

# Let `gh` pick up whichever GitHub token Bitrise CI happens to expose.
if [ -z "${GH_TOKEN:-}" ] && [ -z "${GITHUB_TOKEN:-}" ]; then
  if [ -n "${GITHUB_API_TOKEN:-}" ]; then
    export GH_TOKEN="$GITHUB_API_TOKEN"
  fi
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

git clone --filter=blob:none --no-checkout "https://github.com/${CLI_REPO}.git" "$tmpdir/cli" >/dev/null
git -C "$tmpdir/cli" fetch --tags --no-recurse-submodules origin >/dev/null

extract_const() {
  local content="$1" name="$2"
  printf '%s\n' "$content" \
    | sed -nE "s/.*${name}[[:space:]]*=[[:space:]]*\"([0-9]+\.[0-9]+\.[0-9]+)\".*/\1/p" \
    | head -1
}

# Decode a file from a step repo at a given ref. Prints empty on any failure.
fetch_step_file() {
  local repo="$1" path="$2" ref="$3"
  local b64
  b64=$(gh api "repos/bitrise-steplib/${repo}/contents/${path}?ref=${ref}" --jq '.content' 2>/dev/null || true)
  if [ -z "$b64" ]; then
    return 0
  fi
  printf '%s' "$b64" | base64 -d 2>/dev/null || true
}

# Find the CLI version a step tag pins by probing the three known patterns in
# order. Prints "vX.Y.Z" on first match, empty if the tag doesn't reference the CLI.
find_cli_version_for_step_tag() {
  local repo="$1" ref="$2"
  local content cli

  content=$(fetch_step_file "$repo" "step.sh" "$ref")
  cli=$(printf '%s\n' "$content" \
    | sed -nE 's/.*BITRISE_BUILD_CACHE_CLI_VERSION="(v[0-9]+\.[0-9]+\.[0-9]+)".*/\1/p' \
    | head -1)
  if [ -n "$cli" ]; then printf '%s' "$cli"; return 0; fi

  content=$(fetch_step_file "$repo" "step/cli.go" "$ref")
  cli=$(printf '%s\n' "$content" \
    | sed -nE 's/.*cliVersion[[:space:]]*=[[:space:]]*"([0-9]+\.[0-9]+\.[0-9]+)".*/v\1/p' \
    | head -1)
  if [ -n "$cli" ]; then printf '%s' "$cli"; return 0; fi

  content=$(fetch_step_file "$repo" "go.mod" "$ref")
  # Match only released versions; the trailing `[^-]` rules out Go module
  # pseudo-versions like v1.5.6-0.20260407... which point at a commit, not a tag.
  cli=$(printf '%s\n' "$content" \
    | sed -nE 's#.*github\.com/bitrise-io/bitrise-build-cache-cli(/v[0-9]+)?[[:space:]]+v([0-9]+\.[0-9]+\.[0-9]+)([[:space:]]|$).*#v\2#p' \
    | head -1)
  if [ -n "$cli" ]; then printf '%s' "$cli"; return 0; fi
}

# Build CLI table data: TSV with columns tag\tdate\tanalytics\tcache\ttestdistro\tcommon
cli_data="$tmpdir/cli_data.tsv"
: > "$cli_data"

cli_tags=$(git -C "$tmpdir/cli" tag --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$')
cli_count=0
for tag in $cli_tags; do
  consts=$(git -C "$tmpdir/cli" show "${tag}:${CONSTS_PATH}" 2>/dev/null || true)
  if [ -z "$consts" ]; then
    echo "Skipping $tag (no $CONSTS_PATH at that tag)"
    continue
  fi

  release_date=$(git -C "$tmpdir/cli" log -1 --format=%as "$tag")
  analytics=$(extract_const "$consts" GradleAnalyticsPluginDepVersion)
  cache=$(extract_const "$consts" GradleRemoteBuildCachePluginDepVersion)
  testdistro=$(extract_const "$consts" GradleTestDistributionPluginDepVersion)
  common=$(extract_const "$consts" GradleCommonPluginDepVersion)

  printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$tag" "$release_date" \
    "${analytics:--}" "${cache:--}" "${testdistro:--}" "${common:--}" >> "$cli_data"
  cli_count=$((cli_count + 1))
done

echo "Processed $cli_count CLI tags"
if [ "$cli_count" -eq 0 ]; then
  echo "Zero CLI tags processed — refusing to write an empty matrix"
  exit 1
fi

lookup_cli_row() {
  awk -F'\t' -v t="$1" '$1==t {print; exit}' "$cli_data"
}

cat "$MD_HEADER_PATH" > "$RESULT_MD_PATH"

{
  echo
  echo "## CLI releases → bundled plugin versions"
  echo
  echo "| CLI version | Release date | Analytics plugin | Cache plugin | Test Distribution plugin | Common plugin |"
  echo "|-------------|--------------|------------------|--------------|--------------------------|----------------|"
  while IFS=$'\t' read -r tag release_date analytics cache testdistro common; do
    echo "| [$tag]($CLI_RELEASE_URL_PREFIX/$tag) | $release_date | $analytics | $cache | $testdistro | $common |"
  done < "$cli_data"
} >> "$RESULT_MD_PATH"

for entry in "${STEPS[@]}"; do
  IFS='|' read -r step_name step_repo <<< "$entry"

  echo "Processing step $step_name ($step_repo) ..."

  step_tags=$(gh api "repos/bitrise-steplib/${step_repo}/git/refs/tags" --paginate --jq '.[].ref' 2>/dev/null \
    | sed 's#^refs/tags/##' \
    | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' \
    | sort -V -r || true)

  {
    echo
    echo "## $step_name"
    echo
  } >> "$RESULT_MD_PATH"

  if [ -z "$step_tags" ]; then
    echo "  no stable semver tags"
    echo "No tagged versions yet." >> "$RESULT_MD_PATH"
    continue
  fi

  step_rows="$tmpdir/${step_name}.tsv"
  : > "$step_rows"
  total=0
  matched=0
  for stag in $step_tags; do
    total=$((total + 1))
    cli=$(find_cli_version_for_step_tag "$step_repo" "$stag")
    if [ -z "$cli" ]; then
      continue
    fi

    row=$(lookup_cli_row "$cli")
    if [ -z "$row" ]; then
      printf '%s\t%s\t%s\t%s\t%s\n' "$stag" "$cli" "-" "-" "-" >> "$step_rows"
    else
      analytics=$(printf '%s' "$row" | cut -f3)
      cache=$(printf '%s' "$row" | cut -f4)
      testdistro=$(printf '%s' "$row" | cut -f5)
      printf '%s\t%s\t%s\t%s\t%s\n' "$stag" "$cli" "$analytics" "$cache" "$testdistro" >> "$step_rows"
    fi
    matched=$((matched + 1))
  done

  echo "  $step_name: $total tags, $matched with CLI ref"

  if [ "$matched" -eq 0 ]; then
    echo "No step versions reference the Bitrise Build Cache CLI." >> "$RESULT_MD_PATH"
    continue
  fi

  {
    echo "| Step version | CLI version | Analytics plugin | Cache plugin | Test Distribution plugin |"
    echo "|--------------|-------------|------------------|--------------|--------------------------|"
    while IFS=$'\t' read -r stag cli analytics cache testdistro; do
      echo "| $stag | [$cli]($CLI_RELEASE_URL_PREFIX/$cli) | $analytics | $cache | $testdistro |"
    done < "$step_rows"
  } >> "$RESULT_MD_PATH"
done

echo "Generated $RESULT_MD_PATH"
