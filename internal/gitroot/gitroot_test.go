//go:build unit

package gitroot_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/gitroot"
)

func TestFind(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, root string) (startDir string, wantRoot string)
		wantErrIs error
	}{
		{
			name: "startDir is repo root with .git dir",
			setup: func(t *testing.T, root string) (string, string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))

				return root, root
			},
		},
		{
			name: "startDir is repo root with .git file (worktree)",
			setup: func(t *testing.T, root string) (string, string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /somewhere/else\n"), 0o644))

				return root, root
			},
		},
		{
			name: "walks up multiple levels",
			setup: func(t *testing.T, root string) (string, string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
				deep := filepath.Join(root, "a", "b", "c")
				require.NoError(t, os.MkdirAll(deep, 0o755))

				return deep, root
			},
		},
		{
			name: "no .git anywhere returns ErrNotInGitRepo",
			setup: func(t *testing.T, root string) (string, string) {
				t.Helper()
				sub := filepath.Join(root, "sub")
				require.NoError(t, os.MkdirAll(sub, 0o755))

				return sub, ""
			},
			wantErrIs: gitroot.ErrNotInGitRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use an isolated temp root; wrap in a subdir so "no .git" can't be
			// satisfied by an ancestor .git dir belonging to a parent repo.
			outer := t.TempDir()
			root := filepath.Join(outer, "case")
			require.NoError(t, os.MkdirAll(root, 0o755))

			startDir, wantRoot := tt.setup(t, root)

			got, err := gitroot.Find(startDir, nil)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				return
			}
			require.NoError(t, err)

			wantEval, err := filepath.EvalSymlinks(wantRoot)
			require.NoError(t, err)
			gotEval, err := filepath.EvalSymlinks(got)
			require.NoError(t, err)

			assert.Equal(t, wantEval, gotEval)
		})
	}
}

func TestFind_NonexistentStartDir(t *testing.T) {
	_, err := gitroot.Find(filepath.Join(t.TempDir(), "does-not-exist"), nil)
	require.Error(t, err)
	assert.NotErrorIs(t, err, gitroot.ErrNotInGitRepo)
}

func TestFind_EmptyStartDir(t *testing.T) {
	_, err := gitroot.Find("", nil)
	require.Error(t, err)
}
