#!/bin/bash
set -e

VERSION_FILE="./internal/consts/consts.go"

SED_IN_PLACE_COMMAND=(-i)
if [[ "$OSTYPE" == "darwin"* ]]; then
  SED_IN_PLACE_COMMAND=(-i "")
fi

compare_versions() {
    # Split versions into arrays
    IFS='.' read -r -a current_parts <<< "$1"
    IFS='.' read -r -a latest_parts <<< "$2"

    # Compare version parts
    for i in {0..2}; do
        if (( ${latest_parts[i]} > ${current_parts[i]} )); then
            return 0
        elif (( ${latest_parts[i]} < ${current_parts[i]} )); then
            return 1
        fi
    done

    return 1
}

update() {
    current_version=$(grep "$dep_version_name" "$VERSION_FILE" | sed 's/.*= "\(.*\)"/\1/')
    if [[ -z "$current_version" ]]; then
        echo "Failed to get the current version of $plugin_name plugin"
        exit 1
    fi
    echo "Current version of $plugin_name plugin: $current_version"

    latest_version=$(curl -s "https://s01.oss.sonatype.org/content/repositories/releases/io/bitrise/gradle/$artifact_name/maven-metadata.xml" | xmllint --xpath 'string(//latest)' -)
    if [[ -z "$latest_version" ]]; then
        echo "Failed to get the latest version of $plugin_name plugin"
        exit 1
    fi
    echo "Latest version of $artifact_name plugin: $latest_version"

    if [[ $latest_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        # Compare versions and update if the latest is greater
        if compare_versions "$current_version" "$latest_version"; then
            sed "${SED_IN_PLACE_COMMAND[@]}" "s/$dep_version_name = \".*\"/$dep_version_name = \"$latest_version\"/" "$VERSION_FILE"
            echo "Updated to version $latest_version"
        else
            echo "No update needed. Current version ($current_version) is up-to-date or newer."
        fi
    else
        echo "Latest version ($latest_version) is not a valid SemVer format."
    fi
}

plugin_name='analytics' artifact_name='gradle-analytics' dep_version_name='GradleAnalyticsPluginDepVersion' update
plugin_name='cache' artifact_name='remote-cache' dep_version_name='GradleRemoteBuildCachePluginDepVersion' update
plugin_name='common' artifact_name='common' dep_version_name='GradleCommonPluginDepVersion' update
plugin_name='test-distribution' artifact_name='test-distribution' dep_version_name='GradleTestDistributionPluginDepVersion' update
