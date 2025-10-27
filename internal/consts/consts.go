package consts

const (
	IAD1    = "IAD1"
	ORD1    = "ORD1"
	USEAST1 = "US_EAST1"

	// BitriseAccelerate currently pointing to IAD1, but in the same time, it's environment-aware.
	// It points to the appropriate instance for the respective datacenter when used on VMs managed by Bitrise.
	// More info: https://github.com/bitrise-io/build-prebooting-deployments/blob/production/preboot-reconciler/startup_script_extension_macos_bitvirt.sh#L72
	BitriseAccelerate = "grpcs://bitrise-accelerate.services.bitrise.io"

	AnalyticsServiceEndpoint = "https://xcode-analytics.services.bitrise.io"

	// Gradle Remote Build Cache related consts
	GradleRemoteBuildCachePluginDepVersion = "1.2.22"

	// Gradle Analytics related consts
	GradleAnalyticsPluginDepVersion = "2.1.34"
	GradleAnalyticsEndpoint         = "gradle-analytics.services.bitrise.io"
	GradleAnalyticsHTTPEndpoint     = "https://gradle-sink.services.bitrise.io"
	GradleAnalyticsPort             = 443

	// Gradle Common Plugin version
	GradleCommonPluginDepVersion = "1.0.3"

	// Gradle Test Distribution Plugin version
	GradleTestDistributionPluginDepVersion = "2.1.26"
	GradleTestDistributionEndpoint         = "grpcs://remote-execution-ord.services.bitrise.io"
	GradleTestDistributionKvEndpoint       = "grpcs://build-cache-api-ord.services.bitrise.io"
	GradleTestDistributionPort             = 443
)
