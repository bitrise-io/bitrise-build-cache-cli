//go:build unit

package gradleconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The hook is what makes the job summary work with no workflow step, so its
// presence has to depend on being on GitHub Actions and nothing else.
func Test_jobSummaryCLIPath(t *testing.T) {
	t.Run("empty off GitHub Actions", func(t *testing.T) {
		t.Setenv("GITHUB_STEP_SUMMARY", "")

		assert.Empty(t, jobSummaryCLIPath())
	})

	t.Run("this CLI's own absolute path on GitHub Actions", func(t *testing.T) {
		t.Setenv("GITHUB_STEP_SUMMARY", "/tmp/step-summary.md")

		path := jobSummaryCLIPath()

		assert.NotEmpty(t, path)
		assert.True(t, len(path) > 0 && path[0] == '/', "want an absolute path, got %q", path)
	})
}
