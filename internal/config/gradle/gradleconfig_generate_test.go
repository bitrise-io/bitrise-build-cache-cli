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
			name: "Activated plugins gets values from inventory (CI bakes auth token literal)",
			inventory: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:  "AuthTokenValue",
					Debug:      true,
					AppSlug:    "AppSlugValue",
					CIProvider: "CIProviderValue",
					Version:    "CommonVersionValue",
					CLIPath:    "CLIPathValue",
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
					Usage:           UsageLevelEnabled,
					Version:         "TestDistroVersionValue",
					Endpoint:        "TestDistroEndpointValue",
					KvEndpoint:      "TestDistroKvEndpointValue",
					Port:            321,
					LogLevel:        "TestDistroLogLevelValue",
					ShardSize:       50,
					TestSearchDepth: 3,
				},
			},
			want:    expectedAllPluginsCI,
			wantErr: "",
		},
		{
			name: "Activated plugins on local dev (empty CIProvider) shells out to CLI for auth token",
			inventory: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:  "AuthTokenValue",
					Debug:      true,
					AppSlug:    "AppSlugValue",
					CIProvider: "",
					Version:    "CommonVersionValue",
					CLIPath:    "CLIPathValue",
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
					Usage:           UsageLevelEnabled,
					Version:         "TestDistroVersionValue",
					Endpoint:        "TestDistroEndpointValue",
					KvEndpoint:      "TestDistroKvEndpointValue",
					Port:            321,
					LogLevel:        "TestDistroLogLevelValue",
					ShardSize:       50,
					TestSearchDepth: 3,
				},
			},
			want:    expectedAllPluginsLocal,
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

//nolint:gosec // expected snippet of generated kotlin script for test assertion, not credentials
const expectedAuthTokenResolver = `// Local-dev only: resolve the auth token at build time via the bitrise-build-cache
// CLI so credentials never live in plain text on disk. CI runs (CIProvider set)
// bake the token literal instead — the same token is already in env vars on the
// CI VM, and embedding it keeps the init.kts byte-stable across configuration-cache
// save/restore VMs.
abstract class BitriseAuthTokenSource : org.gradle.api.provider.ValueSource<String, org.gradle.api.provider.ValueSourceParameters.None> {
    @get:javax.inject.Inject abstract val execOps: org.gradle.process.ExecOperations
    override fun obtain(): String {
        val out = java.io.ByteArrayOutputStream()
        val err = java.io.ByteArrayOutputStream()
        val result = execOps.exec {
            commandLine("CLIPathValue", "auth", "token")
            standardOutput = out
            errorOutput = err
            isIgnoreExitValue = true
        }
        if (result.exitValue != 0) {
            System.err.println("bitrise-build-cache auth token exited ${result.exitValue}: ${err.toString().trim()}")
            return ""
        }
        return out.toString().trim()
    }
}

fun org.gradle.api.provider.ProviderFactory.bitriseAuthToken(): String =
    of(BitriseAuthTokenSource::class.java) {}.get()
`

//nolint:gosec // expected snippet of generated kotlin script for test assertion, not credentials
const expectedAllPluginsCI = expectedImports + "\n" +
	"initscript {\n" +
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
        apply<io.bitrise.gradle.cache.BitriseCCachePlugin>()
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
        kvEndpoint.set("TestDistroKvEndpointValue")
        authToken.set("AuthTokenValue")
        logLevel.set("TestDistroLogLevelValue")
        shardSize.set(50)
        testSearchDepth.set(3)
        bitrise {
            appSlug.set("AppSlugValue")
        }
    }

    apply<io.bitrise.gradle.rbe.RBEPlugin>()
}`

//nolint:gosec // expected snippet of generated kotlin script for test assertion, not credentials
const expectedAllPluginsLocal = expectedImports + "\n" +
	expectedAuthTokenResolver +
	"initscript {\n" +
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
            authToken = providers.bitriseAuthToken()
            isPush = true
            debug = true
            blobValidationLevel = "ValidationLevelValue"
            cacheGradleVersion = gradle.gradleVersion
            collectMetadata = false
        }
    }
    rootProject {
        apply<io.bitrise.gradle.cache.BitriseCCachePlugin>()
    }
    rootProject {
        extensions.create("analytics", AnalyticsPluginExtension::class.java)
        extensions.configure(AnalyticsPluginExtension::class.java) {
            endpoint.set("AnalyticsEndpointURLValue:123")
            httpEndpoint.set("AnalyticsHttpEndpointValue")
            grpcEndpoint.set("AnalyticsGRPCEndpointValue")
            authToken.set(providers.bitriseAuthToken())
            dumpEventsToFiles.set(true)
            debug.set(true)
            enabled.set(true)

            providerName.set("")

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
        kvEndpoint.set("TestDistroKvEndpointValue")
        authToken.set(providers.bitriseAuthToken())
        logLevel.set("TestDistroLogLevelValue")
        shardSize.set(50)
        testSearchDepth.set(3)
        bitrise {
            appSlug.set("AppSlugValue")
        }
    }

    apply<io.bitrise.gradle.rbe.RBEPlugin>()
}`
