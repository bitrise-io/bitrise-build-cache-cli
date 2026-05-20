#!/bin/bash

# Generate docs/dependency-matrix.md by:
#   1. Reading internal/consts/consts.go at each CLI release tag (no binary download, no
#      `activate gradle` invocation — so no side effects like the benchmark-phase API call
#      that previously zeroed out the cache plugin column for every CLI >= v1.1.0).
#   2. For each consumer step that bundles the CLI, listing released versions from the
#      bitrise-steplib registry, resolving each version's source commit, and reading
#      either step.sh (bash toolkit) or step/cli.go / go.mod (Go toolkit) at that
#      commit to find the pinned CLI version. Joins back to the per-CLI plugin
#      versions from step 1.

set -eo pipefail

mkdir -p docs
RESULT_MD_PATH="${RESULT_MD_PATH:-docs/dependency-matrix.md}"
MD_HEADER_PATH="${MD_HEADER_PATH:-assets/dependency-matrix-header.md}"
CLI_RELEASE_URL_PREFIX="https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag"

CLI_REPO="bitrise-io/bitrise-build-cache-cli"
CONSTS_PATH="internal/consts/consts.go"
STEPLIB_REPO="bitrise-io/bitrise-steplib"

# Consumer steps that bundle the CLI, identified by their steplib step ID (the same
# id customers reference in their bitrise.yml). Each version we list comes from
# bitrise-steplib's steps/<id>/<version>/step.yml — that file pins both the source
# repo URL and the exact source commit, which we then probe for the CLI ref.
#
# For each source commit we try, in order: step.sh, step/cli.go, go.mod — first match
# wins. (Bash-toolkit steps export BITRISE_BUILD_CACHE_CLI_VERSION in step.sh; the RN
# step hard-codes cliVersion in step/cli.go; other Go-toolkit steps reference the
# CLI module from go.mod.)
STEPS=(
  "activate-build-cache-for-gradle"
  "activate-build-cache-for-react-native"
  "activate-gradle-mirrors"
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

# Decode a file from any GitHub repo at a given ref (or the default branch if ref
# is empty). Prints empty on any failure.
fetch_file() {
  local owner_repo="$1" path="$2" ref="${3:-}"
  local url="repos/${owner_repo}/contents/${path}"
  if [ -n "$ref" ]; then
    url="${url}?ref=${ref}"
  fi
  local b64
  b64=$(gh api "$url" --jq '.content' 2>/dev/null || true)
  if [ -z "$b64" ]; then
    return 0
  fi
  printf '%s' "$b64" | base64 -d 2>/dev/null || true
}

# Find the CLI version a step source commit pins, by probing the three known
# patterns in order. Prints "vX.Y.Z" on first match, empty if no match.
find_cli_version_at_commit() {
  local owner_repo="$1" commit="$2"
  local content cli

  content=$(fetch_file "$owner_repo" "step.sh" "$commit")
  cli=$(printf '%s\n' "$content" \
    | sed -nE 's/.*BITRISE_BUILD_CACHE_CLI_VERSION="(v[0-9]+\.[0-9]+\.[0-9]+)".*/\1/p' \
    | head -1)
  if [ -n "$cli" ]; then printf '%s' "$cli"; return 0; fi

  content=$(fetch_file "$owner_repo" "step/cli.go" "$commit")
  cli=$(printf '%s\n' "$content" \
    | sed -nE 's/.*cliVersion[[:space:]]*=[[:space:]]*"([0-9]+\.[0-9]+\.[0-9]+)".*/v\1/p' \
    | head -1)
  if [ -n "$cli" ]; then printf '%s' "$cli"; return 0; fi

  content=$(fetch_file "$owner_repo" "go.mod" "$commit")
  # Match only released versions; the trailing space/EOL check rules out Go
  # module pseudo-versions like v1.5.6-0.20260407... which point at a commit.
  cli=$(printf '%s\n' "$content" \
    | sed -nE 's#.*github\.com/bitrise-io/bitrise-build-cache-cli(/v[0-9]+)?[[:space:]]+v([0-9]+\.[0-9]+\.[0-9]+)([[:space:]]|$).*#v\2#p' \
    | head -1)
  if [ -n "$cli" ]; then printf '%s' "$cli"; return 0; fi
}

# Extract owner/repo from a github URL: trims https:// and any .git suffix.
github_owner_repo_from_url() {
  printf '%s' "$1" \
    | sed -E 's#^https?://github\.com/##; s#\.git$##; s#/$##'
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
  echo "## CLI release table"
  echo
  echo "| CLI version | Release date | Analytics plugin | Cache plugin | Test Distribution plugin | Common plugin |"
  echo "|-------------|--------------|------------------|--------------|--------------------------|----------------|"
  while IFS=$'\t' read -r tag release_date analytics cache testdistro common; do
    echo "| [$tag]($CLI_RELEASE_URL_PREFIX/$tag) | $release_date | $analytics | $cache | $testdistro | $common |"
  done < "$cli_data"
} >> "$RESULT_MD_PATH"

for step_id in "${STEPS[@]}"; do
  echo "Processing step $step_id ..."

  # Versions published to steplib are directory names under steps/<id>/. Filter to
  # stable X.Y.Z dirs and sort descending so the newest row sits on top.
  step_versions=$(gh api --paginate "repos/${STEPLIB_REPO}/contents/steps/${step_id}" --jq '.[].name' 2>/dev/null \
    | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' \
    | sort -V -r || true)

  {
    echo
    echo "## $step_id"
    echo
  } >> "$RESULT_MD_PATH"

  if [ -z "$step_versions" ]; then
    echo "  no versions in steplib"
    echo "Not yet published to the Bitrise steplib." >> "$RESULT_MD_PATH"
    continue
  fi

  step_rows="$tmpdir/${step_id}.tsv"
  : > "$step_rows"
  total=0
  matched=0
  for sv in $step_versions; do
    total=$((total + 1))

    # The steplib's step.yml pins both the source repo and the exact commit
    # we should read the CLI ref from.
    step_yml=$(fetch_file "$STEPLIB_REPO" "steps/${step_id}/${sv}/step.yml")
    if [ -z "$step_yml" ]; then
      continue
    fi
    source_url=$(printf '%s\n' "$step_yml" | sed -nE 's#^[[:space:]]*(source_code_url|website):[[:space:]]*(.+)$#\2#p' | head -1)
    commit=$(printf '%s\n' "$step_yml" | sed -nE 's#^[[:space:]]*commit:[[:space:]]*(.+)$#\1#p' | head -1)
    owner_repo=$(github_owner_repo_from_url "$source_url")
    if [ -z "$owner_repo" ] || [ -z "$commit" ]; then
      continue
    fi

    cli=$(find_cli_version_at_commit "$owner_repo" "$commit")
    if [ -z "$cli" ]; then
      continue
    fi

    row=$(lookup_cli_row "$cli")
    if [ -z "$row" ]; then
      printf '%s\t%s\t%s\t%s\t%s\n' "$sv" "$cli" "-" "-" "-" >> "$step_rows"
    else
      analytics=$(printf '%s' "$row" | cut -f3)
      cache=$(printf '%s' "$row" | cut -f4)
      testdistro=$(printf '%s' "$row" | cut -f5)
      printf '%s\t%s\t%s\t%s\t%s\n' "$sv" "$cli" "$analytics" "$cache" "$testdistro" >> "$step_rows"
    fi
    matched=$((matched + 1))
  done

  echo "  $step_id: $total versions, $matched with CLI ref"

  if [ "$matched" -eq 0 ]; then
    echo "No published versions reference the Bitrise Build Cache CLI." >> "$RESULT_MD_PATH"
    continue
  fi

  {
    echo "| Step version | CLI version | Analytics plugin | Cache plugin | Test Distribution plugin |"
    echo "|--------------|-------------|------------------|--------------|--------------------------|"
    while IFS=$'\t' read -r sv cli analytics cache testdistro; do
      echo "| $sv | [$cli]($CLI_RELEASE_URL_PREFIX/$cli) | $analytics | $cache | $testdistro |"
    done < "$step_rows"
  } >> "$RESULT_MD_PATH"
done

echo "Generated $RESULT_MD_PATH"
