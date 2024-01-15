package gradleconfig

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateInitGradle(t *testing.T) {
	type args struct {
		endpointURL    string
		authToken      string
		metricsEnabled bool
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
			},
			want:    expectedInitScriptWithMetrics,
			wantErr: "",
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateInitGradle(tt.args.endpointURL, tt.args.authToken, tt.args.metricsEnabled)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

const expectedInitScriptWithMetrics = `initscript {
    repositories {
        mavenCentral()
        maven {
            url 'https://jitpack.io'
        }
        maven {
            url "https://plugins.gradle.org/m2/"
        }
    }

    dependencies {
        classpath 'io.bitrise.gradle:remote-cache:1.+'
        classpath 'io.bitrise.gradle:gradle-analytics:0.+'
    }
}

println('[BITRISE BUILD CACHE] Gradle Metrics collection: on')
rootProject {
    apply plugin: io.bitrise.gradle.analytics.AnalyticsPlugin

    analytics {
        ignoreErrors = false
        bitrise {
            remote {
                authorization = 'AuthT0ken'
                endpoint = 'gradle-analytics.services.bitrise.io'
                port = 443
            }
        }
    }

    // Configure the analytics producer task to run at the end of the build, no matter what tasks are executed
    allprojects {
        tasks.configureEach {
            if (name != "producer") {
                // The producer task is defined in the root project only, but we are in the allprojects {} block,
                // so this special syntax is needed to reference the root project task
                finalizedBy ":producer"
            }
        }
    }
}

import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory

gradle.settingsEvaluated { settings ->
    settings.buildCache {
        local {
            enabled = false
        }

        registerBuildCacheService(BitriseBuildCache.class, BitriseBuildCacheServiceFactory.class)
        remote(BitriseBuildCache.class) {
            endpoint = 'grpcs://remote-build-cache.services.bitrise.io'
            println('[BITRISE BUILD CACHE] endpoint: ' + endpoint)
            authToken = 'AuthT0ken'
            enabled = true
            push = true
            println('[BITRISE BUILD CACHE] push: ' + push)
            debug = true
            blobValidationLevel = 'warning'
        }
    }
}
`

const expectedInitScriptWithoutMetrics = `initscript {
    repositories {
        mavenCentral()
        maven {
            url 'https://jitpack.io'
        }
    }

    dependencies {
        classpath 'io.bitrise.gradle:remote-cache:1.+'
    }
}

import io.bitrise.gradle.cache.BitriseBuildCache
import io.bitrise.gradle.cache.BitriseBuildCacheServiceFactory

gradle.settingsEvaluated { settings ->
    settings.buildCache {
        local {
            enabled = false
        }

        registerBuildCacheService(BitriseBuildCache.class, BitriseBuildCacheServiceFactory.class)
        remote(BitriseBuildCache.class) {
            endpoint = 'grpcs://remote-build-cache.services.bitrise.io'
            println('[BITRISE BUILD CACHE] endpoint: ' + endpoint)
            authToken = 'AuthT0ken'
            enabled = true
            push = true
            println('[BITRISE BUILD CACHE] push: ' + push)
            debug = true
            blobValidationLevel = 'warning'
        }
    }
}
`
