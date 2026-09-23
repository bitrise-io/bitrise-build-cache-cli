//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStartOptions(t *testing.T) {
	t.Parallel()

	t.Run("no opts leaves config zero", func(t *testing.T) {
		t.Parallel()

		cfg := startConfig{}
		assert.Empty(t, cfg.invocationID)
		assert.False(t, cfg.debug)
	})

	t.Run("WithInvocationID sets id", func(t *testing.T) {
		t.Parallel()

		cfg := startConfig{}
		WithInvocationID("abc-123")(&cfg)
		assert.Equal(t, "abc-123", cfg.invocationID)
	})

	t.Run("WithDebug flips flag", func(t *testing.T) {
		t.Parallel()

		cfg := startConfig{}
		WithDebug()(&cfg)
		assert.True(t, cfg.debug)
	})

	t.Run("options compose", func(t *testing.T) {
		t.Parallel()

		cfg := startConfig{}
		for _, opt := range []StartOption{WithDebug(), WithInvocationID("xyz")} {
			opt(&cfg)
		}
		assert.True(t, cfg.debug)
		assert.Equal(t, "xyz", cfg.invocationID)
	})
}
