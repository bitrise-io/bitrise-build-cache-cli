//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_resolveParentInvocationID(t *testing.T) {
	t.Run("flag value takes priority over env", func(t *testing.T) {
		got := resolveParentInvocationID("flag-id", map[string]string{
			"BITRISE_INVOCATION_ID": "env-id",
		})
		assert.Equal(t, "flag-id", got)
	})

	t.Run("falls back to env when flag is empty", func(t *testing.T) {
		got := resolveParentInvocationID("", map[string]string{
			"BITRISE_INVOCATION_ID": "env-id",
		})
		assert.Equal(t, "env-id", got)
	})

	t.Run("returns empty string when both flag and env are absent", func(t *testing.T) {
		got := resolveParentInvocationID("", map[string]string{})
		assert.Equal(t, "", got)
	})
}
