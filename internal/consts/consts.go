package consts

const (
	LAS1    = "LAS1"
	ATL1    = "ATL1"
	IAD1    = "IAD1"
	ORD1    = "ORD1"
	USEAST1 = "US_EAST1"

	// These URLs are internal only (for now) and environment aware.
	// They point to the appropriate instance for the respective datacenter when used on VMs managed by Bitrise.
	// More info: https://github.com/bitrise-io/build-prebooting-deployments/blob/production/preboot-reconciler/startup_script_extension_macos_bitvirt.sh#L58
	CacheInternalEndpointURLUnified = "grpcs://cache.services.bitrise.io:443"
	RBEInternalEndpointURLUnified   = "grpcs://remote-execution.services.bitrise.io:6669"

	// The default URL uses the public endpoint, which might not be context aware
	// When this comment was written it simply pointed to the GCP us-east cache, but geo loadbalancing is planned.
	EndpointURLDefault = "grpcs://bitrise-accelerate.services.bitrise.io"

	AnalyticsServiceEndpoint = "https://xcode-analytics.services.bitrise.io"

	// Gradle Remote Build Cache related consts
	GradleRemoteBuildCachePluginDepVersion = "1.2.19"

	// Gradle Analytics related consts
	GradleAnalyticsPluginDepVersion = "2.1.28"
	GradleAnalyticsEndpoint         = "gradle-analytics.services.bitrise.io"
	GradleAnalyticsHTTPEndpoint     = "https://gradle-sink.services.bitrise.io"
	GradleAnalyticsPort             = 443

	// Gradle Common Plugin version
	GradleCommonPluginDepVersion = "1.0.1"

	// Gradle Test Distribution Plugin version
	GradleTestDistributionPluginDepVersion = "2.1.24"
	GradleTestDistributionEndpoint         = "grpcs://remote-execution-ord.services.bitrise.io"
	GradleTestDistributionKvEndpoint       = "grpcs://build-cache-api-ord.services.bitrise.io"
	GradleTestDistributionPort             = 443
)
