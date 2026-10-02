//go:build unit

package bazelconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestScanForPinnedHelper(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T) (startDir string)
		want   int
		assert func(t *testing.T, matches []PinnedHelperMatch)
	}{
		{
			name:  "no bazelrc anywhere",
			setup: func(t *testing.T) string { return t.TempDir() },
			want:  0,
		},
		{
			name: "bazelrc without helper line",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, ".bazelrc"), "build --remote_cache=grpcs://example\n")

				return dir
			},
			want: 0,
		},
		{
			name: "commented helper line ignored",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, ".bazelrc"),
					"# build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")

				return dir
			},
			want: 0,
		},
		{
			name: "bitrise scope matches",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, ".bazelrc"),
					"build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")

				return dir
			},
			want: 1,
			assert: func(t *testing.T, matches []PinnedHelperMatch) {
				assert.Equal(t, 1, matches[0].LineNumber)
				assert.Contains(t, matches[0].Line, "services.bitrise.io")
			},
		},
		{
			name: "bare bitrise-build-cache binary reference matches",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, ".bazelrc"),
					"build --credential_helper=example.com=/usr/local/bin/bitrise-build-cache\n")

				return dir
			},
			want: 1,
		},
		{
			name: "unrelated credential helper is skipped",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, ".bazelrc"),
					"build --credential_helper=example.com=/usr/local/bin/gh-cred-helper\n")

				return dir
			},
			want: 0,
		},
		{
			name: "legacy tools/bazel.rc scanned",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "tools"), 0o755))
				writeFile(t, filepath.Join(dir, "tools", "bazel.rc"),
					"build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")

				return dir
			},
			want: 1,
		},
		{
			name: "workspace-root bazelrc scanned from nested cwd",
			setup: func(t *testing.T) string {
				root := t.TempDir()
				writeFile(t, filepath.Join(root, "WORKSPACE"), "")
				writeFile(t, filepath.Join(root, ".bazelrc"),
					"build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")
				nested := filepath.Join(root, "sub", "nested")
				require.NoError(t, os.MkdirAll(nested, 0o755))

				return nested
			},
			want: 1,
		},
		{
			name: "no double-count when workspace-root equals pwd",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, "MODULE.bazel"), "")
				writeFile(t, filepath.Join(dir, ".bazelrc"),
					"build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")

				return dir
			},
			want: 1,
		},
		{
			name: "matches survive extra whitespace and trailing args",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, ".bazelrc"),
					"  build   --credential_helper=*.services.bitrise.io=bitrise-build-cache  # trailing\n")

				return dir
			},
			want: 1,
		},
		{
			name: "config-conditional syntax matches",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, ".bazelrc"),
					"build:remote --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")

				return dir
			},
			want: 1,
		},
		{
			name: "tab-separated space form matches",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, ".bazelrc"),
					"build\t--credential_helper\t*.services.bitrise.io=bitrise-build-cache\n")

				return dir
			},
			want: 1,
		},
		{
			name:  "empty start dir returns nothing",
			setup: func(_ *testing.T) string { return "" },
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := tt.setup(t)
			got, err := ScanForPinnedHelper(context.Background(), start, utils.DefaultOsProxy{}, allTracked)
			require.NoError(t, err)
			assert.Len(t, got, tt.want)
			if tt.assert != nil && len(got) > 0 {
				tt.assert(t, got)
			}
		})
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

// allTracked pretends every candidate is git-tracked so the scan tests can
// focus on the parse/collect logic without setting up a real git repo per case.
func allTracked(string) bool { return true }

// TestScanForPinnedHelper_skipsUntrackedFile guards Zsolt's ask: activate
// writes a per-user ~/.bazelrc that git would ignore, and the scan must not
// warn about our own output.
func TestScanForPinnedHelper_skipsUntrackedFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".bazelrc"),
		"build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")

	untracked := func(string) bool { return false }

	got, err := ScanForPinnedHelper(context.Background(), dir, utils.DefaultOsProxy{}, untracked)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestScanForPinnedHelper_defaultTrackerFallsBackWhenGitMissing exercises the
// fallback in DefaultGitTrackedFn: with git absent from PATH the scan errs on
// the side of surfacing the pin so a real commit still emits the warning.
func TestScanForPinnedHelper_defaultTrackerFallsBackWhenGitMissing(t *testing.T) {
	original := lookGit
	t.Cleanup(func() { lookGit = original })
	lookGit = func() (string, bool) { return "", false }

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".bazelrc"),
		"build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")

	got, err := ScanForPinnedHelper(context.Background(), dir, utils.DefaultOsProxy{}, DefaultGitTrackedFn(context.Background()))
	require.NoError(t, err)
	assert.Len(t, got, 1)
}
