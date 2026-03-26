//go:build unit

package reactnative_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

func Test_BuildGradleActivationFn(t *testing.T) {
	t.Run("passes expanded gradle home and cache-enabled params", func(t *testing.T) {
		var capturedGradleHome string
		var capturedParams gradleconfig.ActivateGradleParams

		fn := reactnative.BuildGradleActivationFn(func(
			_ log.Logger,
			gradleHome string,
			_ map[string]string,
			_ func(log.Logger, map[string]string, bool, common.BenchmarkPhaseProvider) (gradleconfig.TemplateInventory, error),
			_ func(gradleconfig.TemplateInventory, string) error,
			_ gradleconfig.GradlePropertiesUpdater,
			params gradleconfig.ActivateGradleParams,
		) error {
			capturedGradleHome = gradleHome
			capturedParams = params
			return nil
		})

		err := fn(mockLogger)

		require.NoError(t, err)
		home, _ := os.UserHomeDir()
		assert.Equal(t, filepath.Join(home, ".gradle"), capturedGradleHome)
		assert.True(t, capturedParams.Cache.Enabled)
		assert.True(t, capturedParams.Cache.PushEnabled)
	})

	t.Run("propagates activation error", func(t *testing.T) {
		activateErr := errors.New("gradle activate failed")

		fn := reactnative.BuildGradleActivationFn(func(
			_ log.Logger, _ string, _ map[string]string,
			_ func(log.Logger, map[string]string, bool, common.BenchmarkPhaseProvider) (gradleconfig.TemplateInventory, error),
			_ func(gradleconfig.TemplateInventory, string) error,
			_ gradleconfig.GradlePropertiesUpdater,
			_ gradleconfig.ActivateGradleParams,
		) error {
			return activateErr
		})

		err := fn(mockLogger)
		assert.ErrorContains(t, err, "gradle activate failed")
	})
}

func Test_BuildXcodeActivationFn(t *testing.T) {
	t.Run("passes xcode params with debug logging flag", func(t *testing.T) {
		var capturedParams xcelerate.Params

		fn := reactnative.BuildXcodeActivationFn(func(
			_ context.Context, _ log.Logger,
			_ utils.OsProxy, _ utils.CommandFunc,
			_ utils.EncoderFactory, _ utils.DecoderFactory,
			params xcelerate.Params, _ map[string]string,
		) error {
			capturedParams = params
			return nil
		})

		err := fn(context.Background(), mockLogger)

		require.NoError(t, err)
		// DebugLogging should match the package-level flag (false in test context)
		assert.False(t, capturedParams.DebugLogging)
	})

	t.Run("propagates activation error", func(t *testing.T) {
		activateErr := errors.New("xcode activate failed")

		fn := reactnative.BuildXcodeActivationFn(func(
			_ context.Context, _ log.Logger,
			_ utils.OsProxy, _ utils.CommandFunc,
			_ utils.EncoderFactory, _ utils.DecoderFactory,
			_ xcelerate.Params, _ map[string]string,
		) error {
			return activateErr
		})

		err := fn(context.Background(), mockLogger)
		assert.ErrorContains(t, err, "xcode activate failed")
	})
}

func Test_BuildCppActivationFn(t *testing.T) {
	t.Run("passes default ccache params", func(t *testing.T) {
		var capturedParams ccacheconfig.Params

		fn := reactnative.BuildCppActivationFn(func(
			_ context.Context, _ log.Logger,
			_ utils.OsProxy, _ utils.CommandFunc,
			_ utils.EncoderFactory,
			params ccacheconfig.Params, _ map[string]string,
		) error {
			capturedParams = params
			return nil
		})

		err := fn(context.Background(), mockLogger)

		require.NoError(t, err)
		assert.Equal(t, ccacheconfig.DefaultParams(), capturedParams)
	})

	t.Run("propagates activation error", func(t *testing.T) {
		activateErr := errors.New("cpp activate failed")

		fn := reactnative.BuildCppActivationFn(func(
			_ context.Context, _ log.Logger,
			_ utils.OsProxy, _ utils.CommandFunc,
			_ utils.EncoderFactory,
			_ ccacheconfig.Params, _ map[string]string,
		) error {
			return activateErr
		})

		err := fn(context.Background(), mockLogger)
		assert.ErrorContains(t, err, "cpp activate failed")
	})
}

func Test_BuildStartStorageHelperFn(t *testing.T) {
	t.Run("passes binary path and storage-helper start args", func(t *testing.T) {
		var capturedName string
		var capturedArgs []string

		fn := reactnative.BuildStartStorageHelperFn(
			func() (string, error) { return "/path/to/binary", nil },
			func(name string, args ...string) (int, error) {
				capturedName = name
				capturedArgs = args
				return 42, nil
			},
		)

		err := fn(context.Background(), mockLogger)

		require.NoError(t, err)
		assert.Equal(t, "/path/to/binary", capturedName)
		assert.Equal(t, []string{"ccache", "storage-helper", "start"}, capturedArgs)
	})

	t.Run("propagates executable lookup error", func(t *testing.T) {
		execErr := errors.New("executable lookup failed")

		fn := reactnative.BuildStartStorageHelperFn(
			func() (string, error) { return "", execErr },
			func(_ string, _ ...string) (int, error) { return 0, nil },
		)

		err := fn(context.Background(), mockLogger)
		assert.ErrorContains(t, err, "executable lookup failed")
	})

	t.Run("propagates process start error", func(t *testing.T) {
		startErr := errors.New("process start failed")

		fn := reactnative.BuildStartStorageHelperFn(
			func() (string, error) { return "/path/to/binary", nil },
			func(_ string, _ ...string) (int, error) { return 0, startErr },
		)

		err := fn(context.Background(), mockLogger)
		assert.ErrorContains(t, err, "process start failed")
	})
}
