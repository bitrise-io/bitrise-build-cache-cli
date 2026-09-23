//go:build unit

package envexport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
)

func TestExport_SetsOsEnv(t *testing.T) {
	t.Parallel()

	exporter := New(map[string]string{}, log.NewLogger())
	exporter.Export("TEST_ENVEXPORT_KEY", "test_value")

	assert.Equal(t, "test_value", os.Getenv("TEST_ENVEXPORT_KEY"))
	t.Cleanup(func() { os.Unsetenv("TEST_ENVEXPORT_KEY") })
}

func TestExportCLIPath_ExportsRunningBinary(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "github_env")

	New(map[string]string{"GITHUB_ENV": envFile}, log.NewLogger()).ExportCLIPath()

	exe, err := os.Executable()
	require.NoError(t, err)

	content, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Equal(t, consts.EnvCLIPath+"="+exe+"\n", string(content))
	assert.Equal(t, exe, os.Getenv(consts.EnvCLIPath))
	t.Cleanup(func() { os.Unsetenv(consts.EnvCLIPath) })
}

func TestExport_WritesToGitHubEnvFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "github_env")

	exporter := New(map[string]string{
		"GITHUB_ENV": envFile,
	}, log.NewLogger())

	exporter.Export("MY_KEY", "my_value")
	exporter.Export("ANOTHER_KEY", "another_value")

	content, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Equal(t, "MY_KEY=my_value\nANOTHER_KEY=another_value\n", string(content))
}

func TestExport_SkipsGitHubEnvWhenNotSet(t *testing.T) {
	t.Parallel()

	exporter := New(map[string]string{}, log.NewLogger())

	// Should not panic or error
	exporter.Export("SOME_KEY", "some_value")
}

func TestExport_HandlesEnvmanFailureGracefully(t *testing.T) {
	t.Parallel()

	exporter := New(map[string]string{}, log.NewLogger())

	// envman is not installed in test environment — should not panic
	exporter.Export("TEST_ENVMAN_KEY", "test_value")

	// Verify the os.Setenv still worked despite envman failure
	assert.Equal(t, "test_value", os.Getenv("TEST_ENVMAN_KEY"))
	t.Cleanup(func() { os.Unsetenv("TEST_ENVMAN_KEY") })
}

func TestRemoveFromShellRC_StripsBlockFromBothFiles(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	bashRC := filepath.Join(tmpHome, ".bashrc")
	zshRC := filepath.Join(tmpHome, ".zshrc")

	bashContent := "user=alice\n# [start] Bitrise Xcelerate\nexport PATH=/xcel:$PATH\n# [end] Bitrise Xcelerate\ntail=bash\n"
	zshContent := "user=zed\n# [start] Bitrise Xcelerate\nexport PATH=/xcel:$PATH\n# [end] Bitrise Xcelerate\ntail=zsh\n"

	require.NoError(t, os.WriteFile(bashRC, []byte(bashContent), 0o644))
	require.NoError(t, os.WriteFile(zshRC, []byte(zshContent), 0o644))

	exporter := New(map[string]string{"HOME": tmpHome}, log.NewLogger())
	exporter.RemoveFromShellRC("Bitrise Xcelerate")

	bashGot, err := os.ReadFile(bashRC)
	require.NoError(t, err)
	assert.Equal(t, "user=alice\ntail=bash\n", string(bashGot))

	zshGot, err := os.ReadFile(zshRC)
	require.NoError(t, err)
	assert.Equal(t, "user=zed\ntail=zsh\n", string(zshGot))
}

func TestRemoveFromShellRC_MissingFilesAreNoOp(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	exporter := New(map[string]string{"HOME": tmpHome}, log.NewLogger())
	exporter.RemoveFromShellRC("Bitrise Xcelerate")

	_, bashErr := os.Stat(filepath.Join(tmpHome, ".bashrc"))
	assert.True(t, os.IsNotExist(bashErr))
	_, zshErr := os.Stat(filepath.Join(tmpHome, ".zshrc"))
	assert.True(t, os.IsNotExist(zshErr))
}

func TestRemoveFromShellRC_KeepsFileWhenBlockIsOnlyContent(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	rcPath := filepath.Join(tmpHome, ".zshrc")
	require.NoError(t, os.WriteFile(rcPath,
		[]byte("# [start] Bitrise Xcelerate\nexport PATH=/x:$PATH\n# [end] Bitrise Xcelerate\n"), 0o644))

	exporter := New(map[string]string{"HOME": tmpHome}, log.NewLogger())
	exporter.RemoveFromShellRC("Bitrise Xcelerate")

	got, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Empty(t, string(got))
}

// Round-trip test: ExportToShellRC then RemoveFromShellRC must leave the RC file
// byte-identical to its pre-write state — no phantom whitespace, no orphan block,
// no leaked state. Mirrors TestChangeAndRemoveBlock_RoundTrip in stringmerge.
func TestExportAndRemoveFromShellRC_RoundTrip(t *testing.T) {
	t.Parallel()

	const blockName = "Bitrise Xcelerate"
	const blockContent = "export PATH=/xcel:$PATH"

	cases := []struct {
		name    string
		preRC   string
		absent  bool
		rcFiles []string
	}{
		{
			name:    "absent RC files",
			absent:  true,
			rcFiles: []string{".bashrc", ".zshrc"},
		},
		{
			name:    "empty pre-existing RC file",
			preRC:   "",
			rcFiles: []string{".bashrc", ".zshrc"},
		},
		{
			name:    "user content before block",
			preRC:   "user=alice\n",
			rcFiles: []string{".bashrc", ".zshrc"},
		},
		{
			name:    "user content before and after block",
			preRC:   "user=alice\nother=stuff\n",
			rcFiles: []string{".bashrc", ".zshrc"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpHome := t.TempDir()
			// Pre-write the RC files (or leave them absent).
			for _, rcName := range tc.rcFiles {
				rcPath := filepath.Join(tmpHome, rcName)
				if tc.absent {
					continue
				}
				require.NoError(t, os.WriteFile(rcPath, []byte(tc.preRC), 0o644))
			}

			exporter := New(map[string]string{"HOME": tmpHome}, log.NewLogger())
			exporter.ExportToShellRC(blockName, blockContent)
			exporter.RemoveFromShellRC(blockName)

			// After round-trip: the file must match the pre-write byte-for-byte.
			// When the file was absent pre-write, ExportToShellRC creates it, so
			// RemoveFromShellRC leaves an empty file behind.
			for _, rcName := range tc.rcFiles {
				rcPath := filepath.Join(tmpHome, rcName)
				got, err := os.ReadFile(rcPath)
				require.NoError(t, err)

				want := tc.preRC
				if tc.absent {
					want = ""
				}
				assert.Equalf(t, want, string(got),
					"Export→Remove must round-trip for %s (pre=%q)", rcName, tc.preRC)
			}
		})
	}
}
