//go:build unit

package machine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestProjectOptedOut(t *testing.T) {
	tests := []struct {
		name          string
		mode          Mode
		writeMarkerIn string // "" = no marker; "cwd" = at cwd; "parent" = at parent
		want          bool
	}{
		{
			name: "mode=always → false (not opted out)",
			mode: ModeAlways,
			want: false,
		},
		{
			name:          "mode=opt-in + marker at cwd → false",
			mode:          ModeOptIn,
			writeMarkerIn: "cwd",
			want:          false,
		},
		{
			name:          "mode=opt-in + marker at parent → false",
			mode:          ModeOptIn,
			writeMarkerIn: "parent",
			want:          false,
		},
		{
			name:          "mode=opt-in + no marker → true (opted out)",
			mode:          ModeOptIn,
			writeMarkerIn: "",
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			require.NoError(t, Write(Config{ProjectMode: tt.mode}, utils.DefaultOsProxy{}, paths.FromHome(home)))

			work := t.TempDir()
			sub := filepath.Join(work, "nested", "leaf")
			require.NoError(t, os.MkdirAll(sub, 0o755))

			switch tt.writeMarkerIn {
			case "cwd":
				require.NoError(t, os.WriteFile(filepath.Join(sub, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
			case "parent":
				require.NoError(t, os.WriteFile(filepath.Join(work, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
			}

			// Use real OsProxy but override Getwd via a mock that returns sub.
			proxy := &mocks.OsProxyMock{
				GetwdFunc: func() (string, error) {
					return sub, nil
				},
				ReadFileIfExistsFunc: utils.DefaultOsProxy{}.ReadFileIfExists,
			}

			got := ProjectOptedOut(proxy, nil)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProjectOptedOut_PathsDefaultError(t *testing.T) {
	// Unset HOME so paths.Default() fails. Setenv restores at test end.
	t.Setenv("HOME", "")

	// os.UserHomeDir reads $HOME on unix and USERPROFILE on windows; clear both.
	t.Setenv("USERPROFILE", "")

	got := ProjectOptedOut(utils.DefaultOsProxy{}, nil)
	assert.False(t, got, "paths.Default error must fall through to not-opted-out (fail-open on config resolution)")
}

func TestProjectOptedOut_GetwdError_SilentSuppress(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, Write(Config{ProjectMode: ModeOptIn}, utils.DefaultOsProxy{}, paths.FromHome(home)))

	proxy := &mocks.OsProxyMock{
		GetwdFunc: func() (string, error) {
			return "", errors.New("boom")
		},
		ReadFileIfExistsFunc: utils.DefaultOsProxy{}.ReadFileIfExists,
	}

	got := ProjectOptedOut(proxy, nil)
	assert.True(t, got, "Getwd failure must silently suppress (opted-out)")
}

func TestProjectOptedOut_FindMarkerError_SilentSuppress(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, Write(Config{ProjectMode: ModeOptIn}, utils.DefaultOsProxy{}, paths.FromHome(home)))

	work := t.TempDir()
	// malformed marker triggers FindMarker to return an error
	require.NoError(t, os.WriteFile(filepath.Join(work, paths.ProjectMarkerFilename), []byte(`{not json`), 0o644))

	proxy := &mocks.OsProxyMock{
		GetwdFunc: func() (string, error) {
			return work, nil
		},
		ReadFileIfExistsFunc: utils.DefaultOsProxy{}.ReadFileIfExists,
	}

	got := ProjectOptedOut(proxy, nil)
	assert.True(t, got, "FindMarker error must silently suppress (opted-out)")
}
