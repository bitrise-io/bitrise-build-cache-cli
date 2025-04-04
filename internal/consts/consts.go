package consts

const (
	LAS1 = "LAS1"
	ATL1 = "ATL1"
	IAD1 = "IAD1"
	ORD1 = "ORD1"

	// These URLs are internal only (for now) and environment aware.
	// They point to the appropriate instance for the respective datacenter when used on VMs managed by Bitrise.
	// More info: https://github.com/bitrise-io/build-prebooting-deployments/blob/production/preboot-reconciler/startup_script_extension_macos_bitvirt.sh#L58
	CacheInternalEndpointURLUnified = "grpcs://cache.services.bitrise.io:443"
	RBEInternalEndpointURLUnified   = "grpcs://remote-execution.services.bitrise.io:6669"

	// The default URL uses the public endpoint, which might not be context aware
	// When this comment was written it simply pointed to the GCP us-east cache, but geo loadbalancing is planned.
	EndpointURLDefault = "grpcs://remote-build-cache.services.bitrise.io"
	EndpointURLLAS1    = CacheInternalEndpointURLUnified
	EndpointURLATL1    = CacheInternalEndpointURLUnified
	EndpointURLIAD1    = CacheInternalEndpointURLUnified
	EndpointURLORD1    = CacheInternalEndpointURLUnified

	RBEEndpointURLIAD1 = RBEInternalEndpointURLUnified
	RBEEndpointURLORD1 = RBEInternalEndpointURLUnified

	AnalyticsServiceEndpoint = "https://xcode-analytics.services.bitrise.io"

	// Gradle Remote Build Cache related consts
	GradleRemoteBuildCachePluginDepVersion = "1.2.17"

	// Gradle Analytics related consts
	GradleAnalyticsPluginDepVersion = "2.1.20"
	GradleAnalyticsEndpoint         = "gradle-analytics.services.bitrise.io"
	GradleAnalyticsHTTPEndpoint     = "https://gradle-sink.services.bitrise.io"
	GradleAnalyticsPort             = 443
)
