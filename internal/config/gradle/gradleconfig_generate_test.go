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
					AuthToken:             "AuthTokenValue",
					Debug:                 true,
					AppSlug:               "AppSlugValue",
					CIProvider:            "",
					Version:               "CommonVersionValue",
					CLIPath:               "CLIPathValue",
					ProjectMarkerFilename: "MarkerFilenameValue",
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

func Test_GenerateInitGradle_ProjectMarkerGate(t *testing.T) {
	localDev := TemplateInventory{
		Common: PluginCommonTemplateInventory{
			CIProvider:            "",
			CLIPath:               "/opt/bitrise-build-cache",
			ProjectMarkerFilename: ".bitrise-build-cache.json",
			Version:               "commonV",
		},
		Cache: CacheTemplateInventory{
			Usage:               UsageLevelEnabled,
			Version:             "cacheV",
			EndpointURLWithPort: "grpcs://cache",
			ValidationLevel:     "warning",
		},
	}

	got, err := localDev.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)

	assert.Contains(t, got, `commandLine("/opt/bitrise-build-cache", "workspace-for", "--path", parameters.rootDir.get())`)
	assert.Contains(t, got, `settings.layout.rootDirectory.file(".bitrise-build-cache.json")`)
	assert.Contains(t, got, `val bitriseWorkspaceSlug = bitriseWorkspaceForRoot(this)`)
	assert.Contains(t, got, `providers.bitriseAuthToken(bitriseWorkspaceSlug)`)
	assert.Contains(t, got, `settings.providers.fileContents(markerFile).asText.orElse("")`)
	assert.Contains(t, got, `parameters.markerContents.set(markerContents)`)

	ci := localDev
	ci.Common.CIProvider = "bitrise"
	ci.Common.AuthToken = "baked-token"

	gotCI, err := ci.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)

	assert.NotContains(t, gotCI, "BitriseWorkspaceForRootSource")
	assert.NotContains(t, gotCI, "bitriseWorkspaceForRoot")
	assert.NotContains(t, gotCI, "bitriseWorkspaceSlug")
	assert.Contains(t, gotCI, `authToken = "baked-token"`)
}

// When only TestDistro is enabled (Cache=none, Analytics=none), the marker-gate
// machinery must not appear — TestDistro auth stays machine-wide in this phase.
func Test_GenerateInitGradle_TestDistroOnly_NoMarkerGate(t *testing.T) {
	inv := TemplateInventory{
		Common: PluginCommonTemplateInventory{
			CIProvider:            "",
			CLIPath:               "/opt/bitrise-build-cache",
			ProjectMarkerFilename: ".bitrise-build-cache.json",
			Version:               "commonV",
		},
		Cache:     CacheTemplateInventory{Usage: UsageLevelNone},
		Analytics: AnalyticsTemplateInventory{Usage: UsageLevelNone},
		TestDistro: TestDistroTemplateInventory{
			Usage:      UsageLevelEnabled,
			Version:    "testDistroV",
			Endpoint:   "TestDistroEndpointValue",
			KvEndpoint: "TestDistroKvEndpointValue",
			LogLevel:   "info",
		},
	}

	got, err := inv.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)

	assert.NotContains(t, got, "bitriseWorkspaceForRoot")
	assert.NotContains(t, got, "bitriseWorkspaceSlug")
	assert.NotContains(t, got, "BitriseWorkspaceForRootSource")
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
abstract class BitriseAuthTokenSource : org.gradle.api.provider.ValueSource<String, BitriseAuthTokenSource.Params> {
    interface Params : org.gradle.api.provider.ValueSourceParameters {
        val workspace: org.gradle.api.provider.Property<String>
    }
    @get:javax.inject.Inject abstract val execOps: org.gradle.process.ExecOperations
    override fun obtain(): String {
        val out = java.io.ByteArrayOutputStream()
        val err = java.io.ByteArrayOutputStream()
        val args = mutableListOf("CLIPathValue", "auth", "token")
        val ws = parameters.workspace.getOrElse("")
        if (ws.isNotEmpty()) args.add("--workspace=$ws")
        val result = execOps.exec {
            commandLine(args)
            standardOutput = out
            errorOutput = err
            isIgnoreExitValue = true
        }
        if (result.exitValue != 0) {
            org.gradle.api.logging.Logging.getLogger("bitrise-build-cache").warn("bitrise-build-cache auth token exited ${result.exitValue}: ${err.toString().trim()}")
            return ""
        }
        return out.toString().trim()
    }
}

fun org.gradle.api.provider.ProviderFactory.bitriseAuthToken(workspace: String = ""): String =
    of(BitriseAuthTokenSource::class.java) { parameters.workspace.set(workspace) }.get()
// Empty on exit 2 (no marker) or exit 1 (parse error, logged before fallback).
abstract class BitriseWorkspaceForRootSource : org.gradle.api.provider.ValueSource<String, BitriseWorkspaceForRootSource.Params> {
    interface Params : org.gradle.api.provider.ValueSourceParameters {
        val rootDir: org.gradle.api.provider.Property<String>
        // markerContents is unused at obtain() time; declaring it as a parameter
        // wires the marker file into the ValueSource's configuration-cache inputs
        // so edits invalidate the cached result on the next build.
        val markerContents: org.gradle.api.provider.Property<String>
    }
    @get:javax.inject.Inject abstract val execOps: org.gradle.process.ExecOperations
    override fun obtain(): String {
        val out = java.io.ByteArrayOutputStream()
        val err = java.io.ByteArrayOutputStream()
        val result = execOps.exec {
            commandLine("CLIPathValue", "workspace-for", "--path", parameters.rootDir.get())
            standardOutput = out
            errorOutput = err
            isIgnoreExitValue = true
        }
        return when (result.exitValue) {
            0 -> out.toString().trim()
            2 -> ""
            else -> {
                org.gradle.api.logging.Logging.getLogger("bitrise-build-cache").warn("bitrise-build-cache workspace-for exited ${result.exitValue}: ${err.toString().trim()} — falling back to machine-wide credentials")
                ""
            }
        }
    }
}

fun bitriseWorkspaceForRoot(settings: org.gradle.api.initialization.Settings): String {
    val markerFile = settings.layout.rootDirectory.file("MarkerFilenameValue")
    val markerContents = settings.providers.fileContents(markerFile).asText.orElse("")
    return settings.providers.of(BitriseWorkspaceForRootSource::class.java) {
        parameters.rootDir.set(settings.rootDir.absolutePath)
        parameters.markerContents.set(markerContents)
    }.get()
}
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
    apply<io.bitrise.gradle.cache.BitriseCCachePlugin>()
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
rootProject {
    extensions.create("rbe", io.bitrise.gradle.rbe.RBEPluginExtension::class.java).with {
        endpoint.set("TestDistroEndpointValue")
        kvEndpoint.set("TestDistroKvEndpointValue")
        // TODO: thread the per-project workspace slug into providers.bitriseAuthToken() when a marker exists.
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
    val bitriseWorkspaceSlug = bitriseWorkspaceForRoot(this)
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint = "CacheEndpointURLValue"
            authToken = providers.bitriseAuthToken(bitriseWorkspaceSlug)
            isPush = true
            debug = true
            blobValidationLevel = "ValidationLevelValue"
            cacheGradleVersion = gradle.gradleVersion
            collectMetadata = false
        }
    }
    apply<io.bitrise.gradle.cache.BitriseCCachePlugin>()
    extensions.create("analytics", AnalyticsPluginExtension::class.java)
    extensions.configure(AnalyticsPluginExtension::class.java) {
        endpoint.set("AnalyticsEndpointURLValue:123")
        httpEndpoint.set("AnalyticsHttpEndpointValue")
        grpcEndpoint.set("AnalyticsGRPCEndpointValue")
        authToken.set(providers.bitriseAuthToken(bitriseWorkspaceSlug))
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
rootProject {
    extensions.create("rbe", io.bitrise.gradle.rbe.RBEPluginExtension::class.java).with {
        endpoint.set("TestDistroEndpointValue")
        kvEndpoint.set("TestDistroKvEndpointValue")
        // TODO: thread the per-project workspace slug into providers.bitriseAuthToken() when a marker exists.
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
