//go:build unit

package ccache

import (
	"context"
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
)

func TestNewActivator_PushEnabledPropagates(t *testing.T) {
	cases := []struct {
		name string
		push bool
	}{
		{name: "push enabled", push: true},
		{name: "push disabled", push: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewActivator(ActivatorParams{PushEnabled: tc.push})

			assert.Equal(t, tc.push, a.pushEnabled)
		})
	}
}

func swapEnsureFn(t *testing.T, fn func(context.Context, log.Logger, []daemonpkg.Service, daemonpkg.EnsureDeps) error) {
	t.Helper()
	prev := ensureFn
	ensureFn = fn
	t.Cleanup(func() { ensureFn = prev })
}

// The helper is never supervised: a launchd job gets its own resource
// coalition and loses to the compiler it serves, so activation starts it on
// demand instead. See docs/daemon-latency.md.
func TestRunDaemonEnsure_wiresNoCcacheHelperService(t *testing.T) {
	var gotServices []daemonpkg.Service

	swapEnsureFn(t, func(_ context.Context, _ log.Logger, services []daemonpkg.Service, _ daemonpkg.EnsureDeps) error {
		gotServices = services

		return nil
	})

	a := NewActivator(ActivatorParams{PushEnabled: true, Envs: map[string]string{}})
	err := a.runDaemonEnsure(t.Context())
	require.NoError(t, err)

	assert.Empty(t, gotServices)
}

func TestCcacheRunDaemonEnsure_propagatesEnsureError(t *testing.T) {
	swapEnsureFn(t, func(context.Context, log.Logger, []daemonpkg.Service, daemonpkg.EnsureDeps) error {
		return errors.New("systemctl enable: bus unavailable")
	})

	a := NewActivator(ActivatorParams{PushEnabled: true, Envs: map[string]string{}})
	err := a.runDaemonEnsure(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bus unavailable")
}
