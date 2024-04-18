package consts

const (
	EndpointURLDefault = "grpcs://remote-build-cache.services.bitrise.io"
	EndpointURLLAS1    = "grpc://las-cache.services.bitrise.io:6666"
	EndpointURLATL1    = "grpc://atl-cache.services.bitrise.io:6666"

	// Gradle Remote Build Cache related consts
	GradleRemoteBuildCachePluginDepVersion = "1.2.3"

	// Gradle Analytics related consts
	GradleAnalyticsPluginDepVersion = "2.1.1"
	GradleAnalyticsEndpoint         = "gradle-analytics.services.bitrise.io"
	GradleAnalyticsHTTPEndpoint     = "https://gradle-sink.services.bitrise.io"
	GradleAnalyticsPort             = 443
)
