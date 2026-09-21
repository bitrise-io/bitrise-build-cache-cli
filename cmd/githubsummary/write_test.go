//go:build unit

package githubsummary

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An included build is a build of its own, so the hook runs once per build.
// Two renders must leave one summary, not two.
func Test_writeBlock_replacesItsOwnSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.md")

	require.NoError(t, writeBlock(path, "## first\n"))
	require.NoError(t, writeBlock(path, "## second\n"))

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, 1, countOccurrences(string(content), blockStart))
	assert.Contains(t, string(content), "## second")
	assert.NotContains(t, string(content), "## first")
}

// Other steps write to the same file, and theirs must survive.
func Test_writeBlock_keepsOtherContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.md")
	require.NoError(t, os.WriteFile(path, []byte("## someone else\n"), 0o644))

	require.NoError(t, writeBlock(path, "## ours\n"))
	require.NoError(t, writeBlock(path, "## ours again\n"))

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Contains(t, string(content), "## someone else")
	assert.Contains(t, string(content), "## ours again")
	assert.NotContains(t, string(content), "## ours\n")
}

func countOccurrences(s, sub string) int {
	count, idx := 0, 0
	for {
		i := indexFrom(s, sub, idx)
		if i < 0 {
			return count
		}
		count++
		idx = i + len(sub)
	}
}

func indexFrom(s, sub string, from int) int {
	if from >= len(s) {
		return -1
	}
	i := len(s)
	for j := from; j+len(sub) <= len(s); j++ {
		if s[j:j+len(sub)] == sub {
			i = j

			return i
		}
	}

	return -1
}
