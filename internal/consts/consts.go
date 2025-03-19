package consts

const (
	// Unified cache endpoints
	UnifiedCacheEndpointURL = "grpcs://cache.services.bitrise.io:443"
	UnifiedRBEEndpointURL   = "grpcs://remote-execution.services.bitrise.io:443"
	
	// Default public endpoint
	EndpointURLDefault = "grpcs://remote-build-cache.services.bitrise.io"
	
	// Datacenter-specific endpoints (all using unified URLs now)
	EndpointURLLAS1    = UnifiedCacheEndpointURL
	EndpointURLATL1    = UnifiedCacheEndpointURL
	EndpointURLIAD1    = UnifiedCacheEndpointURL
	EndpointURLORD1    = UnifiedCacheEndpointURL

	// Datacenter-specific RBE endpoints (all using unified URLs now)
	RBEEndpointURLIAD1 = UnifiedRBEEndpointURL
	RBEEndpointURLORD1 = UnifiedRBEEndpointURL

	AnalyticsServiceEndpoint = "https://xcode-analytics.services.bitrise.io"

	// Gradle Remote Build Cache related consts
	GradleRemoteBuildCachePluginDepVersion = "1.2.14"

	// Gradle Analytics related consts
	GradleAnalyticsPluginDepVersion = "2.1.13"
	GradleAnalyticsEndpoint         = "gradle-analytics.services.bitrise.io"
	GradleAnalyticsHTTPEndpoint     = "https://gradle-sink.services.bitrise.io"
	GradleAnalyticsPort             = 443
)
