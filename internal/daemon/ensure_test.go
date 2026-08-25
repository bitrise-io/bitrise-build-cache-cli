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
	upped        []Service
	restarted    []Service
}

func TestEnsure_configMissing_bootstraps(t *testing.T) {
	// No config file exists → Bootstrap regardless of pushChanged.
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)
	calls := &ensureCallLog{}

	deps := EnsureDeps{
		Envs:              map[string]string{},
		Probe:             func(context.Context, Service) SocketProbe { return ProbeStopped },
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		BootstrapFn: func(_ context.Context, _ log.Logger, services []Service) (InstallResult, Paths, error) {
			calls.bootstrapped = append(calls.bootstrapped, services...)

			return InstallResult{}, dPaths, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{{Name: ServiceXcelerateProxy}}, false, deps)
	require.NoError(t, err)
	assert.Len(t, calls.bootstrapped, 1)
	assert.Empty(t, calls.upped)
	assert.Empty(t, calls.restarted)
}

func TestEnsure_configPresent_notRunning_upsService(t *testing.T) {
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)
	svc := Service{Name: ServiceXcelerateProxy}
	writeConfigFile(t, backend, dPaths, svc)

	calls := &ensureCallLog{}
	deps := EnsureDeps{
		Envs:              map[string]string{},
		Probe:             func(context.Context, Service) SocketProbe { return ProbeStopped },
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		UpFn: func(_ context.Context, _ Backend, _ Paths, services []Service) (ControlResult, error) {
			calls.upped = append(calls.upped, services...)

			return ControlResult{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{svc}, false, deps)
	require.NoError(t, err)
	assert.Len(t, calls.upped, 1)
}

func TestEnsure_configPresent_running_samePush_noop(t *testing.T) {
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)
	svc := Service{Name: ServiceXcelerateProxy}
	writeConfigFile(t, backend, dPaths, svc)

	deps := EnsureDeps{
		Envs:              map[string]string{},
		Probe:             func(context.Context, Service) SocketProbe { return ProbeRunning },
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		UpFn: func(context.Context, Backend, Paths, []Service) (ControlResult, error) {
			t.Fatalf("Up must not be called when running with same push flag")

			return ControlResult{}, nil
		},
		RestartFn: func(context.Context, Backend, Paths, []Service) (ControlResult, error) {
			t.Fatalf("Restart must not be called when running with same push flag")

			return ControlResult{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{svc}, false, deps)
	require.NoError(t, err)
}

func TestEnsure_configPresent_running_pushChanged_restarts(t *testing.T) {
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)
	svc := Service{Name: ServiceXcelerateProxy}
	writeConfigFile(t, backend, dPaths, svc)

	calls := &ensureCallLog{}
	deps := EnsureDeps{
		Envs:              map[string]string{},
		Probe:             func(context.Context, Service) SocketProbe { return ProbeRunning },
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		RestartFn: func(_ context.Context, _ Backend, _ Paths, services []Service) (ControlResult, error) {
			calls.restarted = append(calls.restarted, services...)

			return ControlResult{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{svc}, true, deps)
	require.NoError(t, err)
	assert.Len(t, calls.restarted, 1)
}

func TestEnsure_configPresent_stuck_pushChanged_upsService(t *testing.T) {
	// Not-running trumps push-changed: no point restarting a service that
	// isn't up. Ensure should start it, not restart it.
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)
	svc := Service{Name: ServiceXcelerateProxy}
	writeConfigFile(t, backend, dPaths, svc)

	calls := &ensureCallLog{}
	deps := EnsureDeps{
		Envs:              map[string]string{},
		Probe:             func(context.Context, Service) SocketProbe { return ProbeStuck },
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		UpFn: func(_ context.Context, _ Backend, _ Paths, services []Service) (ControlResult, error) {
			calls.upped = append(calls.upped, services...)

			return ControlResult{}, nil
		},
		RestartFn: func(context.Context, Backend, Paths, []Service) (ControlResult, error) {
			t.Fatalf("Restart must not be called when service isn't currently up")

			return ControlResult{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{svc}, true, deps)
	require.NoError(t, err)
	assert.Len(t, calls.upped, 1)
}

func TestEnsure_skipEnvVar_shortCircuits(t *testing.T) {
	deps := EnsureDeps{
		Envs: map[string]string{EnvSkipEnsure: "1"},
		Probe: func(context.Context, Service) SocketProbe {
			t.Fatalf("Probe must not run when skip env var is set")

			return ProbeStopped
		},
		BackendAndPathsFn: func() (Backend, Paths, error) {
			t.Fatalf("Backend must not be resolved when skip env var is set")

			return nil, Paths{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{{Name: ServiceCcacheHelper}}, true, deps)
	require.NoError(t, err)
}

func TestEnsure_skipEnvVar_fallsBackToProcessEnv(t *testing.T) {
	// Envs map unset — the OR-fallback branch should still short-circuit off
	// the process-level env var so we don't regress the invariant.
	t.Setenv(EnvSkipEnsure, "1")

	deps := EnsureDeps{
		Probe: func(context.Context, Service) SocketProbe {
			t.Fatalf("Probe must not run when process env var is set")

			return ProbeStopped
		},
		BackendAndPathsFn: func() (Backend, Paths, error) {
			t.Fatalf("Backend must not be resolved when process env var is set")

			return nil, Paths{}, nil
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{{Name: ServiceCcacheHelper}}, true, deps)
	require.NoError(t, err)
}

func TestEnsure_emptyServices_isNoOp(t *testing.T) {
	deps := EnsureDeps{Envs: map[string]string{}}
	err := Ensure(context.Background(), log.NewLogger(), nil, true, deps)
	require.NoError(t, err)
}

func TestEnsure_bootstrapFailure_propagates(t *testing.T) {
	home := t.TempDir()
	backend := fakeBackend{name: "launchd"}
	dPaths := NewPathsFromHome(home)

	deps := EnsureDeps{
		Envs:              map[string]string{},
		Probe:             func(context.Context, Service) SocketProbe { return ProbeStopped },
		BackendAndPathsFn: func() (Backend, Paths, error) { return backend, dPaths, nil },
		BootstrapFn: func(context.Context, log.Logger, []Service) (InstallResult, Paths, error) {
			return InstallResult{}, dPaths, errors.New("launchctl bootstrap: permission denied")
		},
	}

	err := Ensure(context.Background(), log.NewLogger(), []Service{{Name: ServiceXcelerateProxy}}, false, deps)
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
