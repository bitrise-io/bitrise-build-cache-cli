//go:build unit

package xcelerate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestResolveCASPluginPath(t *testing.T) {
	cases := []struct {
		name         string
		envs         map[string]string
		developerDir string
		lstat        func(string) (os.FileInfo, error)
		want         string
	}{
		{
			name:         "env wins over anchor + derived",
			envs:         map[string]string{"BITRISE_XCODE_CAS_PLUGIN_PATH": "/env/plugin.dylib"},
			developerDir: "/Applications/Xcode.app/Contents/Developer",
			lstat:        anchorPresent,
			want:         "/env/plugin.dylib",
		},
		{
			name:         "env wins when anchor missing",
			envs:         map[string]string{"BITRISE_XCODE_CAS_PLUGIN_PATH": "/env/plugin.dylib"},
			developerDir: "/Applications/Xcode.app/Contents/Developer",
			lstat:        anchorMissing,
			want:         "/env/plugin.dylib",
		},
		{
			name:         "anchor live returns anchor-rooted path",
			developerDir: "/Applications/Xcode.app/Contents/Developer",
			lstat:        anchorPresent,
			want:         "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib",
		},
		{
			name:         "anchor missing falls back to developer-dir derived",
			developerDir: "/Applications/Xcode.app/Contents/Developer",
			lstat:        anchorMissing,
			want:         "/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib",
		},
		{
			name:         "developer-dir empty + anchor missing returns anchor-rooted literal",
			developerDir: "",
			lstat:        anchorMissing,
			want:         "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			osProxy := &mocks.OsProxyMock{LstatFunc: tc.lstat}
			got := ResolveCASPluginPath(tc.envs, tc.developerDir, osProxy)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveCASPluginPath_lstatArbitraryErrorFallsThrough(t *testing.T) {
	// A non-ErrNotExist error (e.g. permission denied) should still fall through
	// to the derived path, matching how activation treats the anchor as advisory.
	osProxy := &mocks.OsProxyMock{
		LstatFunc: func(_ string) (os.FileInfo, error) {
			return nil, errors.New("boom")
		},
	}
	got := ResolveCASPluginPath(nil, "/dev-dir", osProxy)
	assert.Equal(t, "/dev-dir/usr/lib/libToolchainCASPlugin.dylib", got)
}

func TestResolveCASPluginPath_ReturnsAbsolutePath_WhenDeveloperDirEmpty_AndNoEnvOverride_AndAnchorMissing(t *testing.T) {
	// Regression guard: resolveDeveloperDir may fail (empty string) and the anchor
	// may be absent. The returned path still has to be absolute — a relative
	// "usr/lib/libToolchainCASPlugin.dylib" would silently land in the IDE
	// override xcconfig and poison the toolchain lookup.
	osProxy := &mocks.OsProxyMock{LstatFunc: anchorMissing}
	got := ResolveCASPluginPath(nil, "", osProxy)
	assert.True(t, filepath.IsAbs(got), "expected absolute path, got %q", got)
}

func TestResolveCASPluginPath_ReturnsAnchorRooted_WhenDeveloperDirEmpty(t *testing.T) {
	osProxy := &mocks.OsProxyMock{LstatFunc: anchorMissing}
	got := ResolveCASPluginPath(nil, "", osProxy)
	assert.Equal(t, "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib", got)
}
