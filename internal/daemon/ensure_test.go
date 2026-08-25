//go:build unit

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBackend is a tiny Backend stub for Ensure state-machine tests. Its Name
// drives configPath, so it must match one of the real backends. We pick
// "launchd" (any name would do); the real launchctl is never called because
// Install/Uninstall/Start/Stop are no-ops here.
type fakeBackend struct{ name string }

func (b fakeBackend) Name() string { return b.name }
func (fakeBackend) Install(context.Context, Paths, Service, string) (string, error) {
	return "", nil
}

func (fakeBackend) Uninstall(context.Context, Paths, Service) (string, bool, error) {
	return "", false, nil
}
func (fakeBackend) Start(context.Context, Paths, Service) error { return nil }
func (fakeBackend) Stop(context.Context, Paths, Service) error  { return nil }

// writeConfigFile drops a fake plist/unit at the path configPath() would
// resolve for svc under paths, so Ensure sees "config exists".
func writeConfigFile(t *testing.T, backend Backend, dPaths Paths, svc Service) {
	t.Helper()

	path := configPath(backend, dPaths, svc)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("fake config"), 0o644)) //nolint:gosec // test path
}

type ensureCallLog struct {
	bootstrapped []Service
	restarted    []Service
}

func TestEnsure_configMissing_bootstraps(t *testing.T) {
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)
	calls := &ensureCallLog{}

	deps := EnsureDeps{
		Envs:              map[string]string{},
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		BootstrapFn: func(_ context.Context, _ log.Logger, services []Service) (InstallResult, Paths, error) {
			calls.bootstrapped = append(calls.bootstrapped, services...)

			return InstallResult{}, dPaths, nil
		},
		RestartFn: func(context.Context, Backend, Paths, []Service) (ControlResult, error) {
			t.Fatalf("Restart must not be called when config file is missing")

			return ControlResult{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{{Name: ServiceXcelerateProxy}}, deps)
	require.NoError(t, err)
	assert.Len(t, calls.bootstrapped, 1)
	assert.Empty(t, calls.restarted)
}

func TestEnsure_configPresent_restarts(t *testing.T) {
	// Config file exists → Restart unconditionally, no probe involved.
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)
	svc := Service{Name: ServiceXcelerateProxy}
	writeConfigFile(t, backend, dPaths, svc)

	calls := &ensureCallLog{}
	deps := EnsureDeps{
		Envs:              map[string]string{},
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		BootstrapFn: func(context.Context, log.Logger, []Service) (InstallResult, Paths, error) {
			t.Fatalf("Bootstrap must not be called when config file is present")

			return InstallResult{}, dPaths, nil
		},
		RestartFn: func(_ context.Context, _ Backend, _ Paths, services []Service) (ControlResult, error) {
			calls.restarted = append(calls.restarted, services...)

			return ControlResult{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{svc}, deps)
	require.NoError(t, err)
	assert.Len(t, calls.restarted, 1)
	assert.Empty(t, calls.bootstrapped)
}

func TestEnsure_skipEnvVar_shortCircuits(t *testing.T) {
	deps := EnsureDeps{
		Envs: map[string]string{EnvSkipEnsure: "1"},
		BackendAndPathsFn: func() (Backend, Paths, error) {
			t.Fatalf("Backend must not be resolved when skip env var is set")

			return nil, Paths{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{{Name: ServiceCcacheHelper}}, deps)
	require.NoError(t, err)
}

func TestEnsure_skipEnvVar_fallsBackToProcessEnv(t *testing.T) {
	// Envs map unset — the OR-fallback branch should still short-circuit off
	// the process-level env var so we don't regress the invariant.
	t.Setenv(EnvSkipEnsure, "1")

	deps := EnsureDeps{
		BackendAndPathsFn: func() (Backend, Paths, error) {
			t.Fatalf("Backend must not be resolved when process env var is set")

			return nil, Paths{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{{Name: ServiceCcacheHelper}}, deps)
	require.NoError(t, err)
}

func TestEnsure_emptyServices_isNoOp(t *testing.T) {
	deps := EnsureDeps{Envs: map[string]string{}}
	err := Ensure(context.Background(), log.NewLogger(), nil, deps)
	require.NoError(t, err)
}

func TestEnsure_bootstrapFailure_propagates(t *testing.T) {
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)

	deps := EnsureDeps{
		Envs:              map[string]string{},
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		BootstrapFn: func(context.Context, log.Logger, []Service) (InstallResult, Paths, error) {
			return InstallResult{}, dPaths, errors.New("launchctl bootstrap: permission denied")
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{{Name: ServiceXcelerateProxy}}, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestServicesForTools_correctMappings(t *testing.T) {
	cases := []struct {
		name          string
		needsXcelerate bool
		needsCcache   bool
		wantNames     []string
	}{
		{name: "neither", wantNames: nil},
		{name: "xcelerate only", needsXcelerate: true, wantNames: []string{ServiceXcelerateProxy}},
		{name: "ccache only", needsCcache: true, wantNames: []string{ServiceCcacheHelper}},
		{
			name:           "both",
			needsXcelerate: true,
			needsCcache:    true,
			wantNames:      []string{ServiceXcelerateProxy, ServiceCcacheHelper},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svcs := ServicesForTools(tc.needsXcelerate, tc.needsCcache)
			var gotNames []string
			for _, s := range svcs {
				gotNames = append(gotNames, s.Name)
			}

			assert.Equal(t, tc.wantNames, gotNames)
		})
	}
}
