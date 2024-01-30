package consts

const (
	EndpointURLDefault = "grpcs://remote-build-cache.services.bitrise.io"
	EndpointURLLAS1    = "grpc://las-cache.services.bitrise.io:6666"
	EndpointURLATL1    = "grpc://atl-cache.services.bitrise.io:6666"

	// Sync the major version of this step and the library.
	// Use the latest 1.x version of our dependency, so we don't have to update this definition after every lib release.
	// But don't forget to update this to `2.+` if the library reaches version 2.0!
	GradleRemoteBuildCachePluginDepVersion = "1.+"

	// Sync the major version of this step and the plugin.
	// Use the latest 1.x version of the plugin, so we don't have to update this definition after every plugin release.
	// But don't forget to update this to `2.+` if the library reaches version 2.0!
	GradleAnalyticsPluginDepVersion = "2.0-RC1"
	GradleAnalyticsEndpoint         = "gradle-analytics.services.bitrise.io"
	GradleAnalyticsPort             = 443
)
