package gradleconfig

import (
	_ "embed"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateInitGradle(t *testing.T) {
	type args struct {
		endpointURL         string
		authToken           string
		userPrefs           Preferences
		cacheConfigMetadata common.CacheConfigMetadata
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr string
	}{
		{
			name: "No Auth Token provided",
			args: args{
				endpointURL: "grpcs://bitrise-accelerate.services.bitrise.io",
			},
			want:    "",
			wantErr: "generate init.gradle, error: AuthToken not provided",
		},
		{
			name: "No EndpointURL provided",
			args: args{
				userPrefs: Preferences{
					AuthToken: "AuthT0ken",
					Cache: CachePreferences{
						Usage: UsageLevelEnabled,
					},
				},
			},
			want:    "",
			wantErr: "generate init.gradle, error: EndpointURL not provided",
		},
		{
			name: "Analytics.Enabled = true",
			args: args{
				userPrefs: Preferences{
					IsDebugEnabled: true,
					AuthToken:      "AuthT0ken",
					Cache: CachePreferences{
						EndpointURL:          "grpcs://bitrise-accelerate.services.bitrise.io",
						Usage:                UsageLevelEnabled,
						IsPushEnabled:        true,
						CacheLevelValidation: CacheValidationLevelWarning,
						Metadata: common.CacheConfigMetadata{
							CIProvider: "BestCI",
							RepoURL:    "https://github.com/some/repo",
							// Bitrise CI specific
							BitriseAppID: "BitriseAppID1",
						},
					},
					Analytics: AnalyticsPreferences{
						UsageLevelEnabled,
					},
				},
			},
			want:    expectedInitScriptWithMetrics,
			wantErr: "",
		},
		{
			name: "Analytics.Enabled = true but empty metadata",
			args: args{
				userPrefs: Preferences{
					IsDebugEnabled: true,
					AuthToken:      "AuthT0ken",
					Cache: CachePreferences{
						Usage:                UsageLevelEnabled,
						IsPushEnabled:        true,
						CacheLevelValidation: CacheValidationLevelWarning,
						EndpointURL:          "grpcs://bitrise-accelerate.services.bitrise.io",
						Metadata:             common.CacheConfigMetadata{},
					},
					Analytics: AnalyticsPreferences{
						UsageLevelEnabled,
					},
				},
			},
			want:    expectedInitScriptWithMetricsButNoMetadata,
			wantErr: "",
		},
		{
			name: "Push disabled, debug enabled, metrics disabled",
			args: args{
				userPrefs: Preferences{
					IsDebugEnabled: true,
					AuthToken:      "AuthT0ken",
					Cache: CachePreferences{
						Usage:                UsageLevelEnabled,
						IsPushEnabled:        false,
						CacheLevelValidation: CacheValidationLevelError,
						EndpointURL:          "grpcs://bitrise-accelerate.services.bitrise.io",
						Metadata:             common.CacheConfigMetadata{},
					},
					Analytics: AnalyticsPreferences{
						UsageLevelNone,
					},
				},
			},
			want: expectedInitScriptNoPushYesDebugNoMetrics,
		},
		{
			name: "Push enabled, debug disabled, metrics disabled",
			args: args{
				userPrefs: Preferences{
					IsDebugEnabled: false,
					AuthToken:      "AuthT0ken",
					Cache: CachePreferences{
						Usage:                UsageLevelEnabled,
						IsPushEnabled:        true,
						CacheLevelValidation: CacheValidationLevelError,
						EndpointURL:          "grpcs://bitrise-accelerate.services.bitrise.io",
						Metadata:             common.CacheConfigMetadata{},
					},
					Analytics: AnalyticsPreferences{
						UsageLevelNone,
					},
				},
			},
			want: expectedInitScriptYesPushNoDebugNoMetrics,
		},
		{
			name: "Add every plugin but only as dependency",
			args: args{
				userPrefs: Preferences{
					IsDebugEnabled: false,
					AuthToken:      "AuthT0ken",
					Cache: CachePreferences{
						Usage: UsageLevelDependency,
					},
					Analytics: AnalyticsPreferences{
						UsageLevelDependency,
					},
					TestDistro: TestDistroPreferences{
						UsageLevelDependency,
					},
				},
			},
			want: expectedAllDepOnly,
		},
		{
			name: "Add test distribution plugin",
			args: args{
				userPrefs: Preferences{
					IsDebugEnabled: false,
					AuthToken:      "AuthT0ken",
					AppSlug:        "AppSlug",
					Cache: CachePreferences{
						Usage: UsageLevelNone,
					},
					Analytics: AnalyticsPreferences{
						UsageLevelNone,
					},
					TestDistro: TestDistroPreferences{
						UsageLevelEnabled,
					},
				},
			},
			want: expectedTestDistributionPlugin,
		},
		{
			name: "Add test distribution plugin when app slug is missing",
			args: args{
				userPrefs: Preferences{
					IsDebugEnabled: false,
					AuthToken:      "AuthT0ken",
					Cache: CachePreferences{
						Usage: UsageLevelNone,
					},
					Analytics: AnalyticsPreferences{
						UsageLevelNone,
					},
					TestDistro: TestDistroPreferences{
						UsageLevelEnabled,
					},
				},
			},
			wantErr: "generate init.gradle, error: AppSlug not provided when TestDistroEnabled",
		},
		{
			name: "Add test distribution plugin with debug enabled",
			args: args{
				userPrefs: Preferences{
					IsDebugEnabled: true,
					AuthToken:      "AuthT0ken",
					AppSlug:        "AppSlug",
					Cache: CachePreferences{
						Usage: UsageLevelNone,
					},
					Analytics: AnalyticsPreferences{
						UsageLevelNone,
					},
					TestDistro: TestDistroPreferences{
						UsageLevelEnabled,
					},
				},
			},
			want: expectedTestDistributionPluginWhenDebugEnabled,
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateInitGradle(tt.args.userPrefs)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

const expectedInitScriptWithMetrics = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        maven(url="https://us-maven.pkg.dev/ip-build-cache-prod/build-cache-maven")
        maven(url="https://plugins.gradle.org/m2/")
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:gradle-analytics:2.1.28")
        classpath("io.bitrise.gradle:remote-cache:1.2.19")
    }
}

settingsEvaluated {
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint.set("grpcs://bitrise-accelerate.services.bitrise.io")
            authToken.set("AuthT0ken")
            isPush.set(true)
            debug.set(true)
            blobValidationLevel.set("warning")
            collectMetadata.set(false)
        }
    }
    rootProject {
        apply<io.bitrise.gradle.analytics.AnalyticsPlugin>()
        extensions.configure<io.bitrise.gradle.analytics.AnalyticsPluginExtension>{
            endpoint.set("gradle-analytics.services.bitrise.io:443")
            httpEndpoint.set("https://gradle-sink.services.bitrise.io")
            authToken.set("AuthT0ken")
            dumpEventsToFiles.set(true)
            debug.set(true)

            providerName.set("BestCI")

            bitrise {
                appSlug.set("BitriseAppID1")
            }
        }
    }
}
`

const expectedInitScriptWithMetricsButNoMetadata = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        maven(url="https://us-maven.pkg.dev/ip-build-cache-prod/build-cache-maven")
        maven(url="https://plugins.gradle.org/m2/")
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:gradle-analytics:2.1.28")
        classpath("io.bitrise.gradle:remote-cache:1.2.19")
    }
}

settingsEvaluated {
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint.set("grpcs://bitrise-accelerate.services.bitrise.io")
            authToken.set("AuthT0ken")
            isPush.set(true)
            debug.set(true)
            blobValidationLevel.set("warning")
            collectMetadata.set(false)
        }
    }
    rootProject {
        apply<io.bitrise.gradle.analytics.AnalyticsPlugin>()
        extensions.configure<io.bitrise.gradle.analytics.AnalyticsPluginExtension>{
            endpoint.set("gradle-analytics.services.bitrise.io:443")
            httpEndpoint.set("https://gradle-sink.services.bitrise.io")
            authToken.set("AuthT0ken")
            dumpEventsToFiles.set(true)
            debug.set(true)

            providerName.set("")

            bitrise {
            }
        }
    }
}
`

const expectedInitScriptWithoutMetrics = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        maven(url="https://us-maven.pkg.dev/ip-build-cache-prod/build-cache-maven")
        maven(url="https://plugins.gradle.org/m2/")
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:remote-cache:1.2.19")
    }
}

settingsEvaluated {
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint.set("grpcs://bitrise-accelerate.services.bitrise.io")
            authToken.set("AuthT0ken")
            isEnabled.set(true)
            isPush.set(true)
            debug.set(true)
            blobValidationLevel.set("warning")
        }
    }
}
`

const expectedInitScriptNoPushYesDebugNoMetrics = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        maven(url="https://us-maven.pkg.dev/ip-build-cache-prod/build-cache-maven")
        maven(url="https://plugins.gradle.org/m2/")
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:remote-cache:1.2.19")
    }
}

settingsEvaluated {
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint.set("grpcs://bitrise-accelerate.services.bitrise.io")
            authToken.set("AuthT0ken")
            isPush.set(false)
            debug.set(true)
            blobValidationLevel.set("error")
        }
    }
}
`

const expectedInitScriptYesPushNoDebugNoMetrics = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        maven(url="https://us-maven.pkg.dev/ip-build-cache-prod/build-cache-maven")
        maven(url="https://plugins.gradle.org/m2/")
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:remote-cache:1.2.19")
    }
}

settingsEvaluated {
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint.set("grpcs://bitrise-accelerate.services.bitrise.io")
            authToken.set("AuthT0ken")
            isPush.set(true)
            debug.set(false)
            blobValidationLevel.set("error")
        }
    }
}
`

const expectedAllDepOnly = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:gradle-analytics:2.1.28")
        classpath("io.bitrise.gradle:remote-cache:1.2.19")
        classpath("io.bitrise.gradle:test-distribution:2.1.24")
    }
}

settingsEvaluated {
}
`

const expectedTestDistributionPlugin = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:test-distribution:2.1.24")
    }
}

settingsEvaluated {
}
rootProject {
    extensions.create("rbe", io.bitrise.gradle.rbe.RBEPluginExtension::class.java).with {
        endpoint.set("grpcs://remote-execution-ord.services.bitrise.io:443")
        kvEndpoint.set("grpcs://build-cache-api-ord.services.bitrise.io:443")
        authToken.set("AuthT0ken")
        logLevel.set("warning")
        bitrise {
            appSlug.set("AppSlug")
        }
    }

    apply<io.bitrise.gradle.rbe.RBEPlugin>()
}
`

const expectedTestDistributionPluginWhenDebugEnabled = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:test-distribution:2.1.24")
    }
}

settingsEvaluated {
}
rootProject {
    extensions.create("rbe", io.bitrise.gradle.rbe.RBEPluginExtension::class.java).with {
        endpoint.set("grpcs://remote-execution-ord.services.bitrise.io:443")
        kvEndpoint.set("grpcs://build-cache-api-ord.services.bitrise.io:443")
        authToken.set("AuthT0ken")
        logLevel.set("debug")
        bitrise {
            appSlug.set("AppSlug")
        }
    }

    apply<io.bitrise.gradle.rbe.RBEPlugin>()
}
`
