#!/bin/bash

set -e

mkdir -p docs
export RESULT_MD_PATH="docs/dependency-matrix.md"
export MD_HEADER_PATH="assets/dependency-matrix-header.md"
export CLI_RELEASE_URL_PREFIX="https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag"
export CLI_DOWNLOAD_URL_PREFIX="https://github.com/bitrise-io/bitrise-build-cache-cli/releases/download"

export BITRISE_BUILD_CACHE_WORKSPACE_ID=322a005426441b60
# `activate gradle --test-distribution` requires this; on Bitrise CI it is set automatically.
export BITRISE_APP_SLUG="${BITRISE_APP_SLUG:-dependency-matrix}"

cat $MD_HEADER_PATH > $RESULT_MD_PATH
export tmpdir=$(mktemp -d)

pushd $tmpdir

git clone --filter=blob:none --no-checkout https://github.com/bitrise-io/bitrise-build-cache-cli.git
cd bitrise-build-cache-cli
git fetch --tags --no-recurse-submodules origin
cd ..

export tmp_md="release-table.md"
echo "| CLI version | Release date | Analytics plugin version | Cache plugin version | Test Distribution plugin version |" >> $tmp_md
echo "|-------------|--------------|--------------------------|----------------------|----------------------------------|" >> $tmp_md

# List CLI release tags in descending semver order — only stable vMAJOR.MINOR.PATCH (no -rc, -beta etc.).
cli_versions=$(cd bitrise-build-cache-cli && git tag --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$')

for cli_version in $cli_versions; do
  echo "==== Processing CLI $cli_version ===="

  semver_regex='^v([0-9]+)\.([0-9]+)\.([0-9]+)$'
  if [[ ! $cli_version =~ $semver_regex ]]; then
    echo "Skipping non-semver tag $cli_version"
    continue
  fi
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  patch="${BASH_REMATCH[3]}"

  version_no_v="${cli_version#v}"
  download_url="$CLI_DOWNLOAD_URL_PREFIX/$cli_version/bitrise-build-cache_${version_no_v}_linux_amd64.tar.gz"

  cli_dir=$(mktemp -d)
  if ! curl --retry 3 -sSfL -o "$cli_dir/cli.tar.gz" "$download_url"; then
    echo "Could not download $download_url — skipping"
    rm -rf "$cli_dir"
    continue
  fi
  tar -xzf "$cli_dir/cli.tar.gz" -C "$cli_dir"
  cli_bin="$cli_dir/bitrise-build-cache"
  if [ ! -x "$cli_bin" ]; then
    echo "Binary missing in $cli_dir — skipping"
    rm -rf "$cli_dir"
    continue
  fi

  # Reset gradle init dir so we only see this version's output.
  rm -f "$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts"

  # Match the historical command-name change: `activate gradle` exists from v0.16.10 onwards.
  if (( major > 0 )) || (( major == 0 && minor > 16 )) || (( major == 0 && minor == 16 && patch >= 10 )); then
    "$cli_bin" activate gradle --cache --test-distribution -d || true
  else
    "$cli_bin" enable-for gradle || true
  fi

  init_script="$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts"
  if [ ! -f "$init_script" ]; then
    echo "Init script not generated for $cli_version — skipping"
    rm -rf "$cli_dir"
    continue
  fi

  analytics_version=$(grep 'classpath("io.bitrise.gradle:gradle-analytics:' "$init_script" | sed -n -E 's/.*gradle-analytics:([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')
  cache_version=$(grep 'classpath("io.bitrise.gradle:remote-cache:' "$init_script" | sed -n -E 's/.*remote-cache:([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')
  test_distro_version=$(grep 'classpath("io.bitrise.gradle:test-distribution:' "$init_script" | sed -n -E 's/.*test-distribution:([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')

  release_date=$(cd bitrise-build-cache-cli && git log -1 --format=%as "$cli_version")

  echo "$cli_version → analytics=$analytics_version cache=$cache_version test-distro=$test_distro_version (released $release_date)"

  echo "| [$cli_version]($CLI_RELEASE_URL_PREFIX/$cli_version) | $release_date | ${analytics_version:--} | ${cache_version:--} | ${test_distro_version:--} |" >> $tmp_md

  rm -rf "$cli_dir"
done

popd
cat $MD_HEADER_PATH > $RESULT_MD_PATH
cat $tmpdir/$tmp_md >> $RESULT_MD_PATH

echo "Release table generated in $RESULT_MD_PATH"
