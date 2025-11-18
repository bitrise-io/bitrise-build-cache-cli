package gradleconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_GenerateInitGradle(t *testing.T) {
	tests := []struct {
		name      string
		inventory TemplateInventory
		want      string
		wantErr   string
	}{
		{
			name: "No plugins",
			inventory: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:  "AuthTokenValue",
					Debug:      true,
					AppSlug:    "AppSlugValue",
					CIProvider: "CIProviderValue",
					Version:    "CommonVersionValue",
				},
				Cache: CacheTemplateInventory{
					Usage: UsageLevelNone,
				},
				Analytics: AnalyticsTemplateInventory{
					Usage: UsageLevelNone,
				},
				TestDistro: TestDistroTemplateInventory{
					Usage: UsageLevelNone,
				},
			},
			want:    expectedNoPluginActivated,
			wantErr: "",
		},
		{
			name: "Dep only plugins",
			inventory: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:  "AuthTokenValue",
					Debug:      true,
					AppSlug:    "AppSlugValue",
					CIProvider: "CIProviderValue",
					Version:    "CommonVersionValue",
				},
				Cache: CacheTemplateInventory{
					Usage:   UsageLevelDependency,
					Version: "CacheVersionValue",
				},
				Analytics: AnalyticsTemplateInventory{
					Usage:   UsageLevelDependency,
					Version: "AnalyticsVersionValue",
				},
				TestDistro: TestDistroTemplateInventory{
					Usage:   UsageLevelDependency,
					Version: "TestDistroVersionValue",
				},
			},
			want:    expectedDepOnlyPlugins,
			wantErr: "",
		},
		{
			name: "Activated plugins gets values from inventory",
			inventory: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:  "AuthTokenValue",
					Debug:      true,
					AppSlug:    "AppSlugValue",
					CIProvider: "CIProviderValue",
					Version:    "CommonVersionValue",
				},
				Cache: CacheTemplateInventory{
					Usage:               UsageLevelEnabled,
					Version:             "CacheVersionValue",
					EndpointURLWithPort: "CacheEndpointURLValue",
					IsPushEnabled:       true,
					ValidationLevel:     "ValidationLevelValue",
				},
				Analytics: AnalyticsTemplateInventory{
					Usage:        UsageLevelEnabled,
					Version:      "AnalyticsVersionValue",
					Endpoint:     "AnalyticsEndpointURLValue",
					Port:         123,
					HTTPEndpoint: "AnalyticsHttpEndpointValue",
					GRPCEndpoint: "AnalyticsGRPCEndpointValue",
				},
				TestDistro: TestDistroTemplateInventory{
					Usage:      UsageLevelEnabled,
					Version:    "TestDistroVersionValue",
					Endpoint:   "TestDistroEndpointValue",
					KvEndpoint: "TestDistroKvEndpointValue",
					Port:       321,
					LogLevel:   "TestDistroLogLevelValue",
					ShardSize:  50,
				},
			},
			want:    expectedAllPlugins,
			wantErr: "",
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.inventory.GenerateInitGradle(GradleTemplateProxy())
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

const expectedImports = `import io.bitrise.gradle.analytics.AnalyticsPluginExtension
import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory`

const expectedRepositories = `    repositories {
        mavenLocal()
        maven {
            name = "gradlePlugins"
            url = uri("https://plugins.gradle.org/m2/")
        }
        mavenCentral()
        google()
        maven {
            name = "jitpackIO"
            url = uri("https://jitpack.io")
        }
    }`

const expectedDependencies = `    dependencies {
        classpath("io.bitrise.gradle:common:CommonVersionValue")
        classpath("io.bitrise.gradle:gradle-analytics:AnalyticsVersionValue")
        classpath("io.bitrise.gradle:remote-cache:CacheVersionValue")
        classpath("io.bitrise.gradle:test-distribution:TestDistroVersionValue")
    }`

const expectedNoPluginActivated = "initscript {\n" + expectedRepositories + "\n}"

const expectedDepOnlyPlugins = "initscript {\n" + expectedRepositories + "\n" + expectedDependencies + "\n}"

const expectedAllPlugins = expectedImports +
	"\ninitscript {\n" +
	expectedRepositories + "\n" +
	expectedDependencies + "\n}" +
	`
settingsEvaluated {
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint = "CacheEndpointURLValue"
            authToken = "AuthTokenValue"
            isPush = true
            debug = true
            blobValidationLevel = "ValidationLevelValue"
            cacheGradleVersion = gradle.gradleVersion
            collectMetadata = false
        }
    }
    rootProject {
        extensions.create("analytics", AnalyticsPluginExtension::class.java)
        extensions.configure(AnalyticsPluginExtension::class.java) {
            endpoint.set("AnalyticsEndpointURLValue:123")
            httpEndpoint.set("AnalyticsHttpEndpointValue")
            grpcEndpoint.set("AnalyticsGRPCEndpointValue")
            authToken.set("AuthTokenValue")
            dumpEventsToFiles.set(true)
            debug.set(true)
            enabled.set(true)

            providerName.set("CIProviderValue")

            bitrise {
                appSlug.set("AppSlugValue")
            }
        }
        apply<io.bitrise.gradle.analytics.AnalyticsPlugin>()
    }
}
rootProject {
    extensions.create("rbe", io.bitrise.gradle.rbe.RBEPluginExtension::class.java).with {
        endpoint.set("TestDistroEndpointValue")
        kvEndpoint.set("TestDistroKvEndpointValue:321")
        authToken.set("AuthTokenValue")
        logLevel.set("TestDistroLogLevelValue")
        shardSize.set(50)
        bitrise {
            appSlug.set("AppSlugValue")
        }
    }

    apply<io.bitrise.gradle.rbe.RBEPlugin>()
}`
