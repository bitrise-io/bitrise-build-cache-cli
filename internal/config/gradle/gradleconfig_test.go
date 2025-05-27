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
				endpointURL: "grpcs://remote-build-cache.services.bitrise.io",
			},
			want:    "",
			wantErr: "generate init.gradle, error: AuthToken not provided",
		},
		{
			name: "No EndpointURL provided",
			args: args{
				authToken: "AuthT0ken",
			},
			want:    "",
			wantErr: "generate init.gradle, error: EndpointURL not provided",
		},
		{
			name: "MetricsEnabled=false",
			args: args{
				endpointURL: "grpcs://remote-build-cache.services.bitrise.io",
				authToken:   "AuthT0ken",
				userPrefs: Preferences{
					IsPushEnabled:        true,
					CacheLevelValidation: CacheValidationLevelWarning,
					IsAnalyticsEnabled:   false,
					IsDebugEnabled:       true,
				},
				cacheConfigMetadata: common.CacheConfigMetadata{
					CIProvider: "BestCI",
					RepoURL:    "https://github.com/some/repo",
					// Bitrise CI specific
					BitriseAppID: "BitriseAppID1",
				},
			},
			want:    expectedInitScriptWithoutMetrics,
			wantErr: "",
		},
		{
			name: "MetricsEnabled=true",
			args: args{
				endpointURL: "grpcs://remote-build-cache.services.bitrise.io",
				authToken:   "AuthT0ken",
				userPrefs: Preferences{
					IsPushEnabled:        true,
					CacheLevelValidation: CacheValidationLevelWarning,
					IsAnalyticsEnabled:   true,
					IsDebugEnabled:       true,
				},
				cacheConfigMetadata: common.CacheConfigMetadata{
					CIProvider: "BestCI",
					RepoURL:    "https://github.com/some/repo",
					// Bitrise CI specific
					BitriseAppID: "BitriseAppID1",
				},
			},
			want:    expectedInitScriptWithMetrics,
			wantErr: "",
		},
		{
			name: "MetricsEnabled=true but empty metadata",
			args: args{
				endpointURL: "grpcs://remote-build-cache.services.bitrise.io",
				authToken:   "AuthT0ken",
				userPrefs: Preferences{
					IsPushEnabled:        true,
					CacheLevelValidation: CacheValidationLevelWarning,
					IsAnalyticsEnabled:   true,
					IsDebugEnabled:       true,
				},
				cacheConfigMetadata: common.CacheConfigMetadata{},
			},
			want:    expectedInitScriptWithMetricsButNoMetadata,
			wantErr: "",
		},
		{
			name: "Push disabled, debug enabled, metrics disabled",
			args: args{
				endpointURL: "grpcs://remote-build-cache.services.bitrise.io",
				authToken:   "AuthT0ken",
				userPrefs: Preferences{
					IsPushEnabled:        false,
					CacheLevelValidation: CacheValidationLevelError,
					IsAnalyticsEnabled:   false,
					IsDebugEnabled:       true,
				},
				cacheConfigMetadata: common.CacheConfigMetadata{},
			},
			want: expectedInitScriptNoPushYesDebugNoMetrics,
		},
		{
			name: "Push enabled, debug disabled, metrics disabled",
			args: args{
				endpointURL: "grpcs://remote-build-cache.services.bitrise.io",
				authToken:   "AuthT0ken",
				userPrefs: Preferences{
					IsPushEnabled:        true,
					CacheLevelValidation: CacheValidationLevelError,
					IsAnalyticsEnabled:   false,
					IsDebugEnabled:       false,
				},
				cacheConfigMetadata: common.CacheConfigMetadata{},
			},
			want: expectedInitScriptYesPushNoDebugNoMetrics,
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateInitGradle(tt.args.endpointURL, tt.args.authToken, tt.args.userPrefs, tt.args.cacheConfigMetadata)
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
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:remote-cache:1.2.19")
        classpath("io.bitrise.gradle:gradle-analytics:2.1.27")
    }
}

settingsEvaluated {
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint = "grpcs://remote-build-cache.services.bitrise.io"
            authToken = "AuthT0ken"
            isEnabled = true
            isPush = true
            debug = true
            blobValidationLevel = "warning"
            collectMetadata = false
        }
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
        enabled.set(true)

        providerName.set("BestCI")

        bitrise {
            appSlug.set("BitriseAppID1")
        }
    }
}
`

const expectedInitScriptWithMetricsButNoMetadata = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
        mavenCentral()
        google()
        maven(url="https://jitpack.io")
    }
    dependencies {
        classpath("io.bitrise.gradle:remote-cache:1.2.19")
        classpath("io.bitrise.gradle:gradle-analytics:2.1.27")
    }
}

settingsEvaluated {
    buildCache {
        local {
            isEnabled = false
        }

        registerBuildCacheService(BitriseBuildCache::class.java, BitriseBuildCacheServiceFactory::class.java)
        remote(BitriseBuildCache::class.java) {
            endpoint = "grpcs://remote-build-cache.services.bitrise.io"
            authToken = "AuthT0ken"
            isEnabled = true
            isPush = true
            debug = true
            blobValidationLevel = "warning"
            collectMetadata = false
        }
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
        enabled.set(true)

        providerName.set("")

        bitrise {
        }
    }
}
`

const expectedInitScriptWithoutMetrics = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
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
            endpoint = "grpcs://remote-build-cache.services.bitrise.io"
            authToken = "AuthT0ken"
            isEnabled = true
            isPush = true
            debug = true
            blobValidationLevel = "warning"
        }
    }
}
`

const expectedInitScriptNoPushYesDebugNoMetrics = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
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
            endpoint = "grpcs://remote-build-cache.services.bitrise.io"
            authToken = "AuthT0ken"
            isEnabled = true
            isPush = false
            debug = true
            blobValidationLevel = "error"
        }
    }
}
`

const expectedInitScriptYesPushNoDebugNoMetrics = `import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory 

initscript {
    repositories {
        mavenLocal()
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
            endpoint = "grpcs://remote-build-cache.services.bitrise.io"
            authToken = "AuthT0ken"
            isEnabled = true
            isPush = true
            debug = false
            blobValidationLevel = "error"
        }
    }
}
`
