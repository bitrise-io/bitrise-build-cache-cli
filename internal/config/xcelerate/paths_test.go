//go:build unit

package xcelerate

import (
	"errors"
	"os"
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
