package gradleconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_InitdTemplate_ByteStableAcrossVersionPins asserts the init-script bytes do not change
// when only plugin version pins would differ. The template reads versions via System.getenv at
// Gradle build time, so changing the pinned Go constants between CLI releases must not change
// the written init-script bytes — otherwise Gradle's configuration-cache fingerprint would
// invalidate when a save-shard ran CLI vN and a restore-shard ran CLI vN+1.
func Test_InitdTemplate_ByteStableAcrossVersionPins(t *testing.T) {
	// Two inventories that differ only in CIProvider (which touches the template body) are
	// render-compared under both branches, each with an empty plugin-version inventory, to
	// prove no plugin-version literal appears in the rendered bytes.
	for _, ciProvider := range []string{"", "bitrise"} {
		t.Run("CIProvider="+ciProvider, func(t *testing.T) {
			inv := TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:  "AuthTokenValue",
					CIProvider: ciProvider,
					CLIPath:    "bitrise-build-cache",
				},
				Cache: CacheTemplateInventory{
					Usage:               UsageLevelEnabled,
					EndpointURLWithPort: "endpoint",
					IsPushEnabled:       true,
					ValidationLevel:     "warning",
				},
				Analytics: AnalyticsTemplateInventory{
					Usage:        UsageLevelEnabled,
					Endpoint:     "ae",
					Port:         1,
					HTTPEndpoint: "ah",
					GRPCEndpoint: "ag",
				},
				TestDistro: TestDistroTemplateInventory{
					Usage:      UsageLevelEnabled,
					Endpoint:   "te",
					KvEndpoint: "tk",
					Port:       2,
					LogLevel:   "warning",
					PoolName:   "pool",
				},
			}

			got, err := inv.GenerateInitGradle(GradleTemplateProxy())
			require.NoError(t, err)

			// None of the pinned plugin version literals must appear in the rendered body.
			// All four of them should resolve from env vars at Gradle build time instead.
			for _, versionLiteral := range []string{
				"CommonVersionValue", "AnalyticsVersionValue", "CacheVersionValue", "TestDistroVersionValue",
				"1.2.0", "3.4.3", "2.2.1", "3.0.0", // current pins at authoring time
			} {
				assert.NotContainsf(t, got, versionLiteral,
					"plugin version literal %q leaked into rendered init-script — the save-shard/restore-shard CC-reuse guarantee requires versions to come from env vars at Gradle build time",
					versionLiteral)
			}

			// And the env-var reads must be present.
			for _, envVarName := range []string{
				"BITRISE_BUILD_CACHE_GRADLE_COMMON_PLUGIN_VERSION",
				"BITRISE_BUILD_CACHE_GRADLE_ANALYTICS_PLUGIN_VERSION",
				"BITRISE_BUILD_CACHE_GRADLE_REMOTE_CACHE_PLUGIN_VERSION",
				"BITRISE_BUILD_CACHE_GRADLE_TEST_DISTRIBUTION_PLUGIN_VERSION",
			} {
				assert.Containsf(t, got, `System.getenv("`+envVarName+`")`,
					"init-script must read %s via System.getenv at Gradle build time", envVarName)
			}
		})
	}
}
