#!/bin/bash
set -e

VERSION_FILE="./internal/consts/consts.go"
TEST_FILE="./internal/config/gradle/gradleconfig_test.go"

SED_IN_PLACE_COMMAND='-i'
if [[ "$OSTYPE" == "darwin"* ]]; then
  SED_IN_PLACE_COMMAND='-i ""'
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

# Update gradle analytics
current_version=$(grep 'GradleAnalyticsPluginDepVersion' "$VERSION_FILE" | sed 's/.*= "\(.*\)"/\1/')
if [[ -z "$current_version" ]]; then
    echo "Failed to get the current version of analytics plugin"
    exit 1
fi
echo "Current version of gradle-analytics plugin: $current_version"

latest_version=$(curl -s "https://s01.oss.sonatype.org/content/repositories/releases/io/bitrise/gradle/gradle-analytics/maven-metadata.xml" | xmllint --xpath 'string(//latest)' -)
if [[ -z "$latest_version" ]]; then
    echo "Failed to get the latest version of analytics plugin"
    exit 1
fi

echo "Latest version of gradle-analytics plugin: $latest_version"

if [[ $latest_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    # Compare versions and update if the latest is greater
    if compare_versions "$current_version" "$latest_version"; then
        sed "$SED_IN_PLACE_COMMAND" "s/GradleAnalyticsPluginDepVersion = \".*\"/GradleAnalyticsPluginDepVersion = \"$latest_version\"/" "$VERSION_FILE"
        sed "$SED_IN_PLACE_COMMAND" "s/classpath(\"io.bitrise.gradle:gradle-analytics:.*\")/classpath(\"io.bitrise.gradle:gradle-analytics:$latest_version\")/" "$TEST_FILE"
        echo "Updated to version $latest_version"
    else
        echo "No update needed. Current version ($current_version) is up-to-date or newer."
    fi
else
    echo "Latest version ($latest_version) is not a valid SemVer format."
fi

# Update remote cache
current_version=$(grep 'GradleRemoteBuildCachePluginDepVersion' "$VERSION_FILE" | sed 's/.*= "\(.*\)"/\1/')
if [[ -z "$current_version" ]]; then
    echo "Failed to get the current version of remote cache plugin"
    exit 1
fi
echo "Current version of remote-cache plugin: $current_version"

latest_version=$(curl -s "https://s01.oss.sonatype.org/content/repositories/releases/io/bitrise/gradle/remote-cache/maven-metadata.xml" | xmllint --xpath 'string(//latest)' -)
if [[ -z "$latest_version" ]]; then
    echo "Failed to get the latest version of remote cache plugin"
    exit 1
fi

echo "Latest version of remote-cache plugin: $latest_version"

if [[ $latest_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    # Compare versions and update if the latest is greater
    if compare_versions "$current_version" "$latest_version"; then
        sed "$SED_IN_PLACE_COMMAND" "s/GradleRemoteBuildCachePluginDepVersion = \".*\"/GradleRemoteBuildCachePluginDepVersion = \"$latest_version\"/" "$VERSION_FILE"
        sed "$SED_IN_PLACE_COMMAND" "s/classpath(\"io.bitrise.gradle:remote-cache:.*\")/classpath(\"io.bitrise.gradle:remote-cache:$latest_version\")/" "$TEST_FILE"
        echo "Updated to version $latest_version"
    else
        echo "No update needed. Current version ($current_version) is up-to-date or newer."
    fi
else
    echo "Latest version ($latest_version) is not a valid SemVer format."
fi
