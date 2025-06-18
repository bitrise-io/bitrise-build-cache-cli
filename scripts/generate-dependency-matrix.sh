#!/bin/bash

set -e

mkdir -p docs
export RESULT_MD_PATH="docs/dependency-matrix.md"
export MD_HEADER_PATH="assets/dependency-matrix-header.md"
export CLI_RELEASE_URL_PREFIX="https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag"

export BITRISE_BUILD_CACHE_AUTH_TOKEN=dummy
export BITRISE_BUILD_CACHE_WORKSPACE_ID=dummy

cat $MD_HEADER_PATH > $RESULT_MD_PATH
export tmpdir=$(mktemp -d)

pushd $tmpdir

git clone https://github.com/bitrise-io/bitrise-steplib.git
git clone https://github.com/bitrise-io/bitrise-build-cache-cli.git
git clone https://github.com/bitrise-steplib/bitrise-step-activate-gradle-remote-cache.git

export tmp_md="release-table.md"
echo "| Step version | CLI version | Analytics plugin version | Cache plugin version | Test Distribution plugin version |" >> $tmp_md
echo "|--------------|-------------|--------------------------|----------------------|----------------------------------|" >> $tmp_md


find "bitrise-steplib/steps/activate-build-cache-for-gradle" -mindepth 1 -maxdepth 1 -type d | while read -r dir; do
  basename "$dir"
done |
sort -Vr | while read -r step_version; do
  step_commit=$(grep "commit: " "bitrise-steplib/steps/activate-build-cache-for-gradle/$step_version/step.yml" | sed -n -E 's/.*commit: ([a-f0-9]+).*/\1/p')

  if [[ -z "$step_commit" ]]; then
    echo "Step: $step_version does not have a commit"
    continue
  fi

  echo "Step: $step_version points to step commit $step_commit"

  cd bitrise-step-activate-gradle-remote-cache
  git checkout "$step_commit"

  cli_version=$(grep 'export BITRISE_BUILD_CACHE_CLI_VERSION=' "step.sh" | sed -n -E 's/.*="(v[0-9]+\.[0-9]+\.[0-9]+)".*/\1/p')
  echo "CLI version: $cli_version"

  if [[ -z "$cli_version" ]]; then
      echo "Step: $step_commit does not have a CLI version env var"
      cd ..
      continue
  fi

  cd ../bitrise-build-cache-cli
  git checkout "$cli_version"

  go run main.go activate gradle --cache --test-distribution

  if [ ! -f "$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts" ]; then
    echo "Gradle build cache not enabled in $HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts"
    cd ..
    continue
  fi

  analytics_version=$(grep 'classpath("io.bitrise.gradle:gradle-analytics:' "$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts" | sed -n -E 's/.*gradle-analytics:([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')
  cache_version=$(grep 'classpath("io.bitrise.gradle:remote-cache:' "$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts" | sed -n -E 's/.*remote-cache:([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')
  test_distro_version=$(grep 'classpath("io.bitrise.gradle:test-distribution:' "$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts" | sed -n -E 's/.*test-distribution:([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')

  echo "Gradle build cache enabled with analytics version: $analytics_version, cache version: $cache_version, test-distribution version: $test_distro_version"
  cd ..

  echo "| $step_version | [$cli_version]($CLI_RELEASE_URL_PREFIX/$cli_version) | $analytics_version | $cache_version | $test_distro_version |" >> $tmp_md
done

popd
cat $MD_HEADER_PATH > $RESULT_MD_PATH
cat $tmpdir/$tmp_md >> $RESULT_MD_PATH

echo "Release table generated in $RESULT_MD_PATH"
