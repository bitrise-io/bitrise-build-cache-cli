//go:build unit

package gradleconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// The hook is what makes the job summary work with no workflow step, so its
// presence has to depend on being on GitHub Actions and nothing else.
func Test_jobSummaryEnabled(t *testing.T) {
	t.Run("off outside GitHub Actions", func(t *testing.T) {
		t.Setenv("GITHUB_STEP_SUMMARY", "")

		inv := ActivateGradleParams{}.commonTemplateInventory(
			authpkg.Credential{}, authpkg.Origin{}, common.CacheConfigMetadata{}, false)

		assert.False(t, inv.JobSummaryEnabled)
	})

	t.Run("on when GitHub Actions provides a summary file", func(t *testing.T) {
		t.Setenv("GITHUB_STEP_SUMMARY", "/tmp/step-summary.md")

		inv := ActivateGradleParams{}.commonTemplateInventory(
			authpkg.Credential{}, authpkg.Origin{}, common.CacheConfigMetadata{}, false)

		assert.True(t, inv.JobSummaryEnabled)
	})
}
