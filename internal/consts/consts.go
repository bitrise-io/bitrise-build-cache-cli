package consts

const (
	EndpointURLDefault = "grpcs://remote-build-cache.services.bitrise.io"
	EndpointURLLAS1    = "grpc://las-cache.services.bitrise.io:6666"
	EndpointURLATL1    = "grpc://atl-cache.services.bitrise.io:6666"
	EndpointURLIAD1    = "grpc://iad-cache.services.bitrise.io:6666"
	EndpointURLORD1    = "grpc://ord-cache.services.bitrise.io:6666"

	AnalyticsServiceEndpoint = "https://xcode-analytics.services.bitrise.io"

	// Gradle Remote Build Cache related consts
	GradleRemoteBuildCachePluginDepVersion = "1.2.8"

	// Gradle Analytics related consts
	GradleAnalyticsPluginDepVersion = "2.1.7"
	GradleAnalyticsEndpoint         = "gradle-analytics.services.bitrise.io"
	GradleAnalyticsHTTPEndpoint     = "https://gradle-sink.services.bitrise.io"
	GradleAnalyticsPort             = 443
)
