//go:build unit

package ccache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
)

// The mocks panic on any call, so reaching one means the activator did not stop at the gate.
func TestActivator_StopsBeforeAnyWorkWhenTheWorkspaceHasNoBuildCache(t *testing.T) {
	asked := 0
	a := ccache.NewActivator(ccache.ActivatorParams{
		Envs:    map[string]string{},
		OsProxy: &mocks.OsProxyMock{},
		SkipForEntitlement: func(context.Context) bool {
			asked++

			return true
		},
	})

	require.NoError(t, a.Activate(context.Background()))

	require.Equal(t, 1, asked)
}
