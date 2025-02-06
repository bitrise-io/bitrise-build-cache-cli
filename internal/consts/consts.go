package consts

const (
	EndpointURLDefault = "grpcs://remote-build-cache.services.bitrise.io"
	EndpointURLLAS1    = "grpcs://build-cache-api-iad.services.bitrise.io:443"
	EndpointURLATL1    = "grpcs://build-cache-api-iad.services.bitrise.io:443"
	EndpointURLIAD1    = "grpcs://iad-cache.services.bitrise.io:443"
	EndpointURLORD1    = "grpcs://ord-cache.services.bitrise.io:443"

	RBEEndpointURLIAD1 = "grpcs://rbe-internal-iad.services.bitrise.io:6669"
	RBEEndpointURLORD1 = "grpcs://rbe-internal-ord.services.bitrise.io:6669"

	AnalyticsServiceEndpoint = "https://xcode-analytics.services.bitrise.io"

	// Gradle Remote Build Cache related consts
	GradleRemoteBuildCachePluginDepVersion = "1.2.16"

	// Gradle Analytics related consts
	GradleAnalyticsPluginDepVersion = "2.1.14"
	GradleAnalyticsEndpoint         = "gradle-analytics.services.bitrise.io"
	GradleAnalyticsHTTPEndpoint     = "https://gradle-sink.services.bitrise.io"
	GradleAnalyticsPort             = 443
)
