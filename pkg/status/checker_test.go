//go:build unit

package status_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/status"
)

type featureBits struct {
	gradle, xcode, cpp, rn bool
}

func writeFixture(t *testing.T, home string, b featureBits) {
	t.Helper()

	if b.gradle {
		initDir := filepath.Join(home, ".gradle", "init.d")
		require.NoError(t, os.MkdirAll(initDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(initDir, "bitrise-build-cache.init.gradle.kts"), []byte("// stub"), 0o600))
	}

	if b.xcode {
		dir := filepath.Join(home, ".bitrise-xcelerate")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		payload, err := json.Marshal(xcelerate.Config{BuildCacheEnabled: true})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))
	}

	if b.cpp {
		dir := filepath.Join(home, ".bitrise", "cache", "ccache")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		payload, err := json.Marshal(ccacheconfig.Config{Enabled: true})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))
	}

	if b.rn {
		dir := filepath.Join(home, ".bitrise", "cache", "reactnative")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		payload, err := json.Marshal(rnconfig.Config{Enabled: true})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))
	}
}

func newCheckerForHome(home string) *status.Checker {
	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		OpenFileFunc:    os.OpenFile,
		StatFunc:        os.Stat,
	}
	decoderFactory := &utilsMocks.DecoderFactoryMock{
		DecoderFunc: func(r io.Reader) utils.Decoder { return json.NewDecoder(r) },
	}

	return status.NewChecker(status.CheckerParams{
		Logger:         mockLogger,
		OsProxy:        osProxy,
		DecoderFactory: decoderFactory,
	})
}

func TestChecker_Status_Matrix(t *testing.T) {
	cases := []struct {
		name string
		bits featureBits
		want status.Status
	}{
		{
			name: "nothing activated",
			bits: featureBits{},
			want: status.Status{},
		},
		{
			name: "only gradle",
			bits: featureBits{gradle: true},
			want: status.Status{Gradle: true},
		},
		{
			name: "only xcode",
			bits: featureBits{xcode: true},
			want: status.Status{Xcode: true},
		},
		{
			name: "only cpp",
			bits: featureBits{cpp: true},
			want: status.Status{Cpp: true},
		},
		{
			name: "only react-native",
			bits: featureBits{rn: true},
			want: status.Status{ReactNative: true},
		},
		{
			name: "everything",
			bits: featureBits{gradle: true, xcode: true, cpp: true, rn: true},
			want: status.Status{Gradle: true, Xcode: true, Cpp: true, ReactNative: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			writeFixture(t, home, tc.bits)

			got := newCheckerForHome(home).Status()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestChecker_XcodeDisabled_WhenBuildCacheFlagFalse(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	payload, err := json.Marshal(xcelerate.Config{BuildCacheEnabled: false})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))

	got := newCheckerForHome(home).Status()
	assert.False(t, got.Xcode)
}

func TestChecker_CppDisabled_WhenEnabledFlagFalse(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".bitrise", "cache", "ccache")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	payload, err := json.Marshal(ccacheconfig.Config{Enabled: false})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))

	got := newCheckerForHome(home).Status()
	assert.False(t, got.Cpp)
}

func TestChecker_IsEnabled(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, home, featureBits{rn: true})
	c := newCheckerForHome(home)

	rn, err := c.IsEnabled(status.FeatureReactNative)
	require.NoError(t, err)
	assert.True(t, rn)

	gradle, err := c.IsEnabled(status.FeatureGradle)
	require.NoError(t, err)
	assert.False(t, gradle)

	_, err = c.IsEnabled("bazel")
	require.Error(t, err)
	assert.True(t, errors.Is(err, status.ErrUnknownFeature))

	_, err = c.IsEnabled("nonsense")
	require.Error(t, err)
	assert.True(t, errors.Is(err, status.ErrUnknownFeature))
}

func TestNewChecker_Defaults(t *testing.T) {
	c := status.NewChecker(status.CheckerParams{})
	require.NotNil(t, c)
	// default OsProxy will look at the real $HOME; we only assert no crash.
	_ = c.Status()
}
