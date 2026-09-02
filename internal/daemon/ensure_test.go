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

// fakeBackend.Name drives configPath, so it must match a real backend name.
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
	clearCIEnv(t)

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
	clearCIEnv(t)

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
	clearCIEnv(t)

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
		name           string
		needsXcelerate bool
		needsCcache    bool
		wantNames      []string
	}{
		// Activation supervises nothing. The proxy is measured -- a launchd
		// job gets its own resource coalition and loses to the compiler it
		// serves. The ccache helper is not measured as harmful; it is
		// unsupervised for consistency and caution. See docs/daemon-latency.md.
		{name: "neither", wantNames: nil},
		{name: "xcelerate only", needsXcelerate: true, wantNames: nil},
		{name: "ccache only", needsCcache: true, wantNames: nil},
		{name: "both", needsXcelerate: true, needsCcache: true, wantNames: nil},
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

// A supervised proxy on CI serves cache operations orders of magnitude slower
// than the wrapper-started one, so CI must not install the service at all.
func TestEnsure_SkipsOnCI(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"bitrise", "BITRISE_IO", "true"},
		{"github actions", "GITHUB_ACTIONS", "true"},
		{"circleci", "CIRCLECI", "true"},
		{"gitlab", "GITLAB_CI", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearCIEnv(t)
			envs := map[string]string{tc.key: tc.value}
			if tc.key == "BITRISE_IO" {
				envs["BITRISE_BUILD_SLUG"] = "slug"
			}

			called := false
			err := Ensure(context.Background(), log.NewLogger(), DefaultServices(), EnsureDeps{
				Envs: envs,
				BackendAndPathsFn: func() (Backend, Paths, error) {
					called = true

					return nil, Paths{}, nil
				},
			})

			require.NoError(t, err)
			assert.False(t, called, "must not touch the service manager on CI")
		})
	}
}

// Off CI the daemon is still the supported lifecycle.
func TestEnsure_RunsWhenNotOnCI(t *testing.T) {
	clearCIEnv(t)

	called := false
	_ = Ensure(context.Background(), log.NewLogger(), DefaultServices(), EnsureDeps{
		Envs: map[string]string{},
		BackendAndPathsFn: func() (Backend, Paths, error) {
			called = true

			return nil, Paths{}, assert.AnError
		},
	})

	assert.True(t, called, "a developer machine still gets the supervised proxy")
}

// These tests run on CI themselves, so the ambient CI variables have to go.
func clearCIEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{"CIRCLECI", "GITHUB_ACTIONS", "GITLAB_CI", "BITRISE_IO", "BITRISE_BUILD_SLUG"} {
		t.Setenv(key, "")
	}
}
