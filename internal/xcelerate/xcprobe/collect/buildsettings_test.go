//go:build unit

package collect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodebuildinfo"
)

func TestBuildSettingsCollector_noClient_noOp(t *testing.T) {
	out, err := BuildSettingsCollector{}.Collect(context.Background(), Input{Scheme: "App"})
	require.NoError(t, err)
	assert.Nil(t, out.BuildSettings)
}

func TestBuildSettingsCollector_noScheme_noOp(t *testing.T) {
	c := xcodebuildinfo.New("")
	out, err := BuildSettingsCollector{}.Collect(context.Background(), Input{XcodebuildInfo: c})
	require.NoError(t, err)
	assert.Nil(t, out.BuildSettings)
}

func TestParseShowBuildSettings_plucksKnownFields(t *testing.T) {
	raw := []byte(`[
	  {
	    "target": "App",
	    "buildSettings": {
	      "SWIFT_OPTIMIZATION_LEVEL": "-Onone",
	      "SWIFT_COMPILATION_MODE": "incremental",
	      "SWIFT_WHOLE_MODULE_OPTIMIZATION": "NO",
	      "GCC_OPTIMIZATION_LEVEL": "0",
	      "ENABLE_BITCODE": "NO",
	      "DEBUG_INFORMATION_FORMAT": "dwarf-with-dsym",
	      "IPHONEOS_DEPLOYMENT_TARGET": "17.0",
	      "ARCHS": "arm64",
	      "VALID_ARCHS": "arm64 arm64e"
	    }
	  },
	  {
	    "target": "Dependency",
	    "buildSettings": { "SWIFT_OPTIMIZATION_LEVEL": "ignored" }
	  }
	]`)

	snap, err := parseShowBuildSettings(raw)
	require.NoError(t, err)
	require.NotNil(t, snap)

	assert.Equal(t, "-Onone", snap.SwiftOptimizationLevel)
	assert.Equal(t, "incremental", snap.SwiftCompilationMode)
	assert.Equal(t, "NO", snap.SwiftWholeModuleOptimization)
	assert.Equal(t, "0", snap.GCCOptimizationLevel)
	assert.Equal(t, "NO", snap.EnableBitcode)
	assert.Equal(t, "dwarf-with-dsym", snap.DebugInformationFormat)
	assert.Equal(t, "17.0", snap.DeploymentTarget)
	assert.Equal(t, "arm64", snap.Archs)
	assert.Equal(t, "arm64 arm64e", snap.ValidArchs)
}

func TestParseShowBuildSettings_picksFirstPlatformDeploymentTarget(t *testing.T) {
	raw := []byte(`[{
	  "target": "MacApp",
	  "buildSettings": {
	    "MACOSX_DEPLOYMENT_TARGET": "14.0",
	    "IPHONEOS_DEPLOYMENT_TARGET": ""
	  }
	}]`)

	snap, err := parseShowBuildSettings(raw)
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, "14.0", snap.DeploymentTarget)
}

func TestParseShowBuildSettings_emptyArray_nil(t *testing.T) {
	snap, err := parseShowBuildSettings([]byte("[]"))
	require.NoError(t, err)
	assert.Nil(t, snap)
}

func TestParseShowBuildSettings_malformed_errors(t *testing.T) {
	_, err := parseShowBuildSettings([]byte("not-json"))
	require.Error(t, err)
}
