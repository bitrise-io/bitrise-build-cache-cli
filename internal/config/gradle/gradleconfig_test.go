package gradleconfig

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

func TestGenerateInitGradle(t *testing.T) {
	type args struct {
		endpointURL         string
		authToken           string
		metricsEnabled      bool
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
				endpointURL:    "grpcs://remote-build-cache.services.bitrise.io",
				authToken:      "AuthT0ken",
				metricsEnabled: false,
				cacheConfigMetadata: common.CacheConfigMetadata{
					CIProvider: "BestCI",
					RepoURL:    "https://github.com/some/repo",
					// Bitrise CI specific
					BitriseAppID:        "BitriseAppID1",
					BitriseBuildID:      "BitriseBuildID1",
					BitriseWorkflowName: "BitriseWorkflowName1",
				},
			},
			want:    expectedInitScriptWithoutMetrics,
			wantErr: "",
		},
		{
			name: "MetricsEnabled=true",
			args: args{
				endpointURL:    "grpcs://remote-build-cache.services.bitrise.io",
				authToken:      "AuthT0ken",
				metricsEnabled: true,
				cacheConfigMetadata: common.CacheConfigMetadata{
					CIProvider: "BestCI",
					RepoURL:    "https://github.com/some/repo",
					// Bitrise CI specific
					BitriseAppID:        "BitriseAppID1",
					BitriseBuildID:      "BitriseBuildID1",
					BitriseWorkflowName: "BitriseWorkflowName1",
				},
			},
			want:    expectedInitScriptWithMetrics,
			wantErr: "",
		},
		{
			name: "MetricsEnabled=true but empty metadata",
			args: args{
				endpointURL:         "grpcs://remote-build-cache.services.bitrise.io",
				authToken:           "AuthT0ken",
				metricsEnabled:      true,
				cacheConfigMetadata: common.CacheConfigMetadata{},
			},
			want:    expectedInitScriptWithMetricsButNoMetadata,
			wantErr: "",
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateInitGradle(tt.args.endpointURL, tt.args.authToken, tt.args.metricsEnabled, tt.args.cacheConfigMetadata)
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
        classpath("io.bitrise.gradle:remote-cache:1.2.4")
        classpath("io.bitrise.gradle:gradle-analytics:2.1.3")
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
            buildSlug.set("BitriseBuildID1")
            workflowName.set("BitriseWorkflowName1")
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
        classpath("io.bitrise.gradle:remote-cache:1.2.4")
        classpath("io.bitrise.gradle:gradle-analytics:2.1.3")
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
        classpath("io.bitrise.gradle:remote-cache:1.2.4")
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
        }
    }
}
`
