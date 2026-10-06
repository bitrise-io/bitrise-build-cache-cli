//go:build unit

package reactnative

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Without the gate this would install dependencies and activate every tool.
func TestActivator_StopsBeforeAnyWorkWhenTheWorkspaceHasNoBuildCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	asked := 0
	a := NewActivator(ActivatorParams{
		GradleEnabled: true,
		XcodeEnabled:  true,
		CppEnabled:    true,
		SkipForEntitlement: func(context.Context) bool {
			asked++

			return true
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	require.NoError(t, a.Activate(ctx))

	require.Equal(t, 1, asked)
}
