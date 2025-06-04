package cmd

import (
    "fmt"
    "os"
    "path/filepath"
    "testing"

    "github.com/bitrise-io/go-utils/v2/log"
    "github.com/bitrise-io/go-utils/v2/mocks"
    "github.com/bitrise-io/go-utils/v2/pathutil"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"
)

func Test_activateForGradleCmdFn(t *testing.T) {
    prep := func() (log.Logger, string) {
        mockLogger := &mocks.Logger{}
        mockLogger.On("Infof", mock.Anything).Return()
        mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
        mockLogger.On("Debugf", mock.Anything).Return()
        mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
        mockLogger.On("Errorf", mock.Anything).Return()
        mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
        tmpPath := t.TempDir()
        tmpGradleHomeDir := filepath.Join(tmpPath, ".gradle")

        activateForGradleParams = DefaultActivateForGradleParams()
        isDebugLogMode = false

        return mockLogger, tmpGradleHomeDir
    }

    t.Run("Envs not specified", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{})
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.EqualError(t, err, "read auth config from environment variables: BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
    })

    t.Run("Envs specified", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        isInitFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        assert.True(t, isInitFileExists)
    })

    t.Run("By default activates analytics", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
    }
}

settingsEvaluated {
    rootProject {
        apply<io.bitrise.gradle.analytics.AnalyticsPlugin>()
        extensions.configure<io.bitrise.gradle.analytics.AnalyticsPluginExtension>{
            endpoint.set("gradle-analytics.services.bitrise.io:443")
            httpEndpoint.set("https://gradle-sink.services.bitrise.io")
            authToken.set("WorkspaceIDValue:AuthTokenValue")
            dumpEventsToFiles.set(false)
            debug.set(false)

            providerName.set("")

            bitrise {
            }
        }
    }
}
`
        assert.Equal(t, expected, initGradlecontent)
    })

    t.Run("Inserting cache as dep only skips validation of cache inputs", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.Cache.JustDependency = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
}
`
        assert.Equal(t, expected, initGradlecontent)
    })

    t.Run("Activating cache will create gradle.properties", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.Cache.Enabled = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
            authToken = "WorkspaceIDValue:AuthTokenValue"
            isPush = false
            debug = false
            blobValidationLevel = "warning"
        }
    }
}
`
        assert.Equal(t, expected, initGradlecontent)
        //
        isPropertiesFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "gradle.properties"))
        require.NoError(t, err)
        assert.True(t, isPropertiesFileExists)
    })

    t.Run("Activating cache will transfer params set", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.Cache.Enabled = true
        activateForGradleParams.Cache.ValidationLevel = "error"
        activateForGradleParams.Cache.PushEnabled = true
        activateForGradleParams.Cache.Endpoint = "EndpointValue"
        isDebugLogMode = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
            endpoint = "EndpointValue"
            authToken = "WorkspaceIDValue:AuthTokenValue"
            isPush = true
            debug = true
            blobValidationLevel = "error"
        }
    }
}
`
        assert.Equal(t, expected, initGradlecontent)
        //
        isPropertiesFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "gradle.properties"))
        require.NoError(t, err)
        assert.True(t, isPropertiesFileExists)
    })

    t.Run("Activating cache will ignore dep only param", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.Cache.Enabled = true
        activateForGradleParams.Cache.JustDependency = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
            authToken = "WorkspaceIDValue:AuthTokenValue"
            isPush = false
            debug = false
            blobValidationLevel = "warning"
        }
    }
}
`
        assert.Equal(t, expected, initGradlecontent)
        //
        isPropertiesFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "gradle.properties"))
        require.NoError(t, err)
        assert.True(t, isPropertiesFileExists)
    })

    t.Run("Activating cache will error on invalid cache validation level", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.Cache.Enabled = true
        activateForGradleParams.Cache.ValidationLevel = "invalid"

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.EqualError(t, err, "invalid cache validation level, valid options: none, warning, error")
        //
        isPropertiesFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "gradle.properties"))
        assert.False(t, isPropertiesFileExists)
    })

    t.Run("Analytics can be added as dependency only", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = false // true by default
        activateForGradleParams.Analytics.JustDependency = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
    }
}

settingsEvaluated {
}
`
        assert.Equal(t, expected, initGradlecontent)
    })

    t.Run("Activating cache will error on invalid cache validation level", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.Cache.Enabled = true
        activateForGradleParams.Cache.ValidationLevel = "invalid"

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.EqualError(t, err, "invalid cache validation level, valid options: none, warning, error")
        //
        isPropertiesFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "gradle.properties"))
        assert.False(t, isPropertiesFileExists)
    })

    t.Run("Enabling Analytics ignores dep only params", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = true
        activateForGradleParams.Analytics.JustDependency = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
    }
}

settingsEvaluated {
    rootProject {
        apply<io.bitrise.gradle.analytics.AnalyticsPlugin>()
        extensions.configure<io.bitrise.gradle.analytics.AnalyticsPluginExtension>{
            endpoint.set("gradle-analytics.services.bitrise.io:443")
            httpEndpoint.set("https://gradle-sink.services.bitrise.io")
            authToken.set("WorkspaceIDValue:AuthTokenValue")
            dumpEventsToFiles.set(false)
            debug.set(false)

            providerName.set("")

            bitrise {
            }
        }
    }
}
`
        assert.Equal(t, expected, initGradlecontent)
    })

    t.Run("Activating Analytics transfers env vars", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
            "BITRISE_IO":                       "true",
            "BITRISE_APP_SLUG":                 "AppSlugValue",
        })

        activateForGradleParams.Analytics.Enabled = true
        activateForGradleParams.Analytics.JustDependency = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
    }
}

settingsEvaluated {
    rootProject {
        apply<io.bitrise.gradle.analytics.AnalyticsPlugin>()
        extensions.configure<io.bitrise.gradle.analytics.AnalyticsPluginExtension>{
            endpoint.set("gradle-analytics.services.bitrise.io:443")
            httpEndpoint.set("https://gradle-sink.services.bitrise.io")
            authToken.set("WorkspaceIDValue:AuthTokenValue")
            dumpEventsToFiles.set(false)
            debug.set(false)

            providerName.set("")

            bitrise {
                appSlug.set("AppSlugValue")
            }
        }
    }
}
`
        assert.Equal(t, expected, initGradlecontent)
    })

    t.Run("One can add test distribution as a dependency only", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.TestDistro.JustDependency = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
`
        assert.Equal(t, expected, initGradlecontent)
    })

    t.Run("Test distribution can be activated", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
            "BITRISE_APP_SLUG":                 "AppSlugValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.TestDistro.Enabled = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
        authToken.set("WorkspaceIDValue:AuthTokenValue")
        logLevel.set("warning")
        bitrise {
            appSlug.set("AppSlugValue")
        }
    }

    apply<io.bitrise.gradle.rbe.RBEPlugin>()
}
`
        assert.Equal(t, expected, initGradlecontent)
    })

    t.Run("Activating test distribution ignores dep only param", func(t *testing.T) {
        mockLogger, tmpGradleHomeDir := prep()
        envVars := createEnvProvider(map[string]string{
            "BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
            "BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
            "BITRISE_APP_SLUG":                 "AppSlugValue",
        })

        activateForGradleParams.Analytics.Enabled = false
        activateForGradleParams.TestDistro.JustDependency = true
        activateForGradleParams.TestDistro.Enabled = true

        // when
        err := activateForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

        // then
        require.NoError(t, err)
        //
        initGradlecontent, err := ReadActivatedPluginsFromInitGradle(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
        require.NoError(t, err)
        expected := `import io.bitrise.gradle.cache.BitriseBuildCache
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
        authToken.set("WorkspaceIDValue:AuthTokenValue")
        logLevel.set("warning")
        bitrise {
            appSlug.set("AppSlugValue")
        }
    }

    apply<io.bitrise.gradle.rbe.RBEPlugin>()
}
`
        assert.Equal(t, expected, initGradlecontent)
    })
}

func ReadActivatedPluginsFromInitGradle(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        // handle the error
        return "", fmt.Errorf("failed to read init gradle from path %s", path)
    }

    return string(data), nil
}
