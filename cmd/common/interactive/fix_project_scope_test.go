//go:build unit

package interactive

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFixProjectScopePrompt_ReturnsCallableFn(t *testing.T) {
	t.Parallel()

	fn := FixProjectScopePrompt(context.Background(), silentLogger())
	assert.NotNil(t, fn, "doctor --fix --interactive wires this into ProjectScopePrompt; a nil would strand the fixer")
}
