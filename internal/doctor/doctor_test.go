//go:build unit

package doctor

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/toolconfig"
)

// ──────────────────────────── fakes ────────────────────────────

type fakeKeyring struct {
	store      map[string]string
	setErr     error
	getErr     error
	deleteErr  error
	getReturns string
}

func (f *fakeKeyring) key(s, a string) string { return s + "|" + a }

func (f *fakeKeyring) Set(s, a, secret string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.store[f.key(s, a)] = secret

	return nil
}

func (f *fakeKeyring) Get(s, a string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	if f.getReturns != "" {
		return f.getReturns, nil
	}
	v, ok := f.store[f.key(s, a)]
	if !ok {
		return "", errors.New("not found")
	}

	return v, nil
}

func (f *fakeKeyring) Delete(s, a string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.store, f.key(s, a))

	return nil
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{store: map[string]string{}} }

type fakeAuthLoader struct {
	creds keychain.Credentials
	err   error
}

func (f fakeAuthLoader) Load() (keychain.Credentials, error) { return f.creds, f.err }

// ──────────────────────────── Overall + version ────────────────────────────

func TestReport_Overall(t *testing.T) {
	tests := []struct {
		name  string
		items []ReportItem
		want  State
	}{
		{"empty", nil, StateOK},
		{"all ok", []ReportItem{{Result: Result{State: StateOK}}}, StateOK},
		{"warn", []ReportItem{{Result: Result{State: StateOK}}, {Result: Result{State: StateWarn}}}, StateWarn},
		{"error wins", []ReportItem{{Result: Result{State: StateWarn}}, {Result: Result{State: StateError}}}, StateError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Report{Items: tt.items}.Overall())
		})
	}
}

func TestIsLocalBuild(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"devel", true},
		{"", false},
		{"v2.8.3", false},
		{"v2.8.4-0.20260603111341-4d0e865a220a+dirty", true},
		{"v2.8.4+local", true},
		{"v2.9.0-rc1", false},
	}
	for _, tt := range tests {
		t.Run(tt.v, func(t *testing.T) {
			assert.Equal(t, tt.want, isLocalBuild(tt.v))
		})
	}
}

// ──────────────────────────── auth ────────────────────────────

func newMinimalDoctor(t *testing.T) *Doctor {
	t.Helper()

	return &Doctor{
		Envs:               map[string]string{},
		AuthLoader:         fakeAuthLoader{creds: keychain.Credentials{AuthToken: "t", WorkspaceID: "w"}},
		Keyring:            newFakeKeyring(),
		CLIVersion:         "devel",
		HTTPClient:         &http.Client{},
		LatestReleaseTag:   func(context.Context, *http.Client) (string, error) { return "", nil },
		LookPath:           func(string) (string, error) { return "/usr/local/bin/ccache", nil },
		StateDirCandidates: []string{},
		ActivatedTools:     func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{} },
		BackendProbe: func(context.Context, common.CacheAuthConfig, map[string]string) (time.Duration, error) {
			return time.Millisecond, nil
		},
	}
}

func TestAuthCheck_keychainWins(t *testing.T) {
	r := newMinimalDoctor(t)
	r.AuthLoader = fakeAuthLoader{creds: keychain.Credentials{AuthToken: "tok", WorkspaceID: "ws-kc"}}

	res := r.authCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "OS keychain")
	assert.Contains(t, res.Detail, "ws-kc")
}

func TestAuthCheck_envFallback(t *testing.T) {
	r := newMinimalDoctor(t)
	r.AuthLoader = fakeAuthLoader{err: keychain.ErrNotFound}
	r.Envs = map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "env-tok",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "ws-env",
	}

	res := r.authCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "environment variables")
	assert.Contains(t, res.Detail, "ws-env")
}

func TestAuthCheck_missingIsError(t *testing.T) {
	r := newMinimalDoctor(t)
	r.AuthLoader = fakeAuthLoader{err: keychain.ErrNotFound}

	res := r.authCheck().Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
	assert.True(t, res.Fixable, "missing creds → Fix re-launches the activate wizard")
}

func TestAuthCheck_fixerIsAuthPromptFixer(t *testing.T) {
	r := newMinimalDoctor(t)
	r.AuthLoader = fakeAuthLoader{err: keychain.ErrNotFound}

	res := r.authCheck().Diagnose(context.Background())
	require.IsType(t, AuthPromptFixer{}, res.Fixer)
}

// ──────────────────────────── keychain smoke ────────────────────────────

func TestKeychainSmokeCheck_happy(t *testing.T) {
	r := &Doctor{Keyring: newFakeKeyring()}
	res := r.keychainSmokeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
}

func TestKeychainSmokeCheck_setFails(t *testing.T) {
	r := &Doctor{Keyring: &fakeKeyring{store: map[string]string{}, setErr: errors.New("dbus down")}}
	res := r.keychainSmokeCheck().Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
}

func TestKeychainSmokeCheck_getMismatch(t *testing.T) {
	r := &Doctor{Keyring: &fakeKeyring{store: map[string]string{}, getReturns: "wrong"}}
	res := r.keychainSmokeCheck().Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
	assert.Contains(t, res.Detail, "mismatched")
}

func TestKeychainSmokeCheck_deleteFailIsWarn(t *testing.T) {
	r := &Doctor{Keyring: &fakeKeyring{store: map[string]string{}, deleteErr: errors.New("denied")}}
	res := r.keychainSmokeCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
}

// ──────────────────────────── xcelerate proxy ────────────────────────────

// xcelerateProxyPidPath returns the proxy.pid path resolved through paths
// against the per-test HOME override.
func xcelerateProxyPidPath(t *testing.T, home string) string {
	t.Helper()
	t.Setenv("HOME", home)

	return filepath.Join(home, ".bitrise-xcelerate", "proxy.pid")
}

func TestXcelerateProxyCheck_noPidIsFixableViaDaemonUp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	r := &Doctor{ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Xcelerate: true} }}
	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.True(t, res.Fixable, "no pid file → Fix runs `daemon up` to start the service")
}

func TestXcelerateProxyCheck_skippedWhenNotActivated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	r := &Doctor{ActivatedTools: func() map[toolconfig.Tool]bool { return nil }}
	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "skipped")
}

func TestXcelerateProxyCheck_stalePidIsFixable(t *testing.T) {
	home := t.TempDir()
	pidPath := xcelerateProxyPidPath(t, home)
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o755))
	require.NoError(t, os.WriteFile(pidPath, []byte(strconv.Itoa(99999999)), 0o600))

	r := &Doctor{ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Xcelerate: true} }}
	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.True(t, res.Fixable)
}

func TestXcelerateProxyCheck_fixRemovesStaleFile(t *testing.T) {
	home := t.TempDir()
	pidPath := xcelerateProxyPidPath(t, home)
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o755))
	require.NoError(t, os.WriteFile(pidPath, []byte("99999999"), 0o600))

	r := &Doctor{ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Xcelerate: true} }}
	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	require.NotNil(t, res.Fixer)

	detail, err := res.Fixer.Fix()
	require.NoError(t, err)
	assert.Contains(t, detail, "removed")

	_, err = os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err))
}

func TestXcelerateProxyCheck_corruptPidIsFixable(t *testing.T) {
	home := t.TempDir()
	pidPath := xcelerateProxyPidPath(t, home)
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o755))
	require.NoError(t, os.WriteFile(pidPath, []byte("not-a-number"), 0o600))

	r := &Doctor{}
	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.True(t, res.Fixable)
}

// ──────────────────────────── ccache ────────────────────────────

func TestCcacheHelperCheck_noSocketIsWarn(t *testing.T) {
	r := &Doctor{Envs: map[string]string{"BITRISE_CCACHE_IPC_SOCKET_PATH": filepath.Join(t.TempDir(), "missing.sock")}}
	res := r.ccacheHelperCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
}

func TestCcacheBinaryCheck_present(t *testing.T) {
	r := &Doctor{LookPath: func(string) (string, error) { return "/usr/local/bin/ccache", nil }}
	res := r.ccacheBinaryCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
}

func TestCcacheBinaryCheck_missingIsWarn(t *testing.T) {
	r := &Doctor{LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	res := r.ccacheBinaryCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
}

// ──────────────────────────── log-dirs ────────────────────────────

func TestLogDirsCheck_missingIsFixable(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	r := &Doctor{StateDirCandidates: []string{filepath.Join(tmp, "a"), filepath.Join(tmp, "b")}}
	res := r.logDirsCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.True(t, res.Fixable)
}

func TestLogDirsCheck_FixCreatesMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	r := &Doctor{StateDirCandidates: []string{filepath.Join(tmp, "new-a")}}
	res := r.logDirsCheck().Diagnose(context.Background())
	require.NotNil(t, res.Fixer)
	_, err := res.Fixer.Fix()
	require.NoError(t, err)
	_, statErr := os.Stat(filepath.Join(tmp, "new-a"))
	assert.NoError(t, statErr)
}

// ──────────────────────────── version ────────────────────────────

func TestCLIVersionCheck_localBuildSkipsNetwork(t *testing.T) {
	r := &Doctor{
		CLIVersion: "v2.8.4-0.xxx+dirty",
		HTTPClient: &http.Client{},
		LatestReleaseTag: func(context.Context, *http.Client) (string, error) {
			t.Fatal("LatestReleaseTag should not be called for local builds")

			return "", nil
		},
	}

	res := r.cliVersionCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State, "local builds should warn — customers shouldn't run self-built CLIs")
	assert.Contains(t, res.Detail, "local build")
}

func TestCLIVersionCheck_behindWarns(t *testing.T) {
	r := &Doctor{
		CLIVersion:       "v2.8.0",
		HTTPClient:       &http.Client{},
		LatestReleaseTag: func(context.Context, *http.Client) (string, error) { return "v2.8.3", nil },
	}

	res := r.cliVersionCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
}

func TestCLIVersionCheck_networkErrorWarns(t *testing.T) {
	r := &Doctor{
		CLIVersion:       "v2.8.3",
		HTTPClient:       &http.Client{},
		LatestReleaseTag: func(context.Context, *http.Client) (string, error) { return "", errors.New("offline") },
	}

	res := r.cliVersionCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
}

// ──────────────────────────── Run + Options ────────────────────────────

func TestRun_appliesFixesWhenAsked(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "logs")

	r := newMinimalDoctor(t)
	r.StateDirCandidates = []string{missing}

	report := r.Run(context.Background(), Options{ApplyFixes: true, SkipUpdateCheck: true})

	var found bool
	for _, it := range report.Items {
		if it.Name == "log-dirs" && it.FixResult != nil {
			found = true
		}
	}
	assert.True(t, found, "log-dirs fix should have been applied")
	_, err := os.Stat(missing)
	assert.NoError(t, err)
}

func TestRun_skipsFixesWhenNotAsked(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "logs")

	r := newMinimalDoctor(t)
	r.StateDirCandidates = []string{missing}

	report := r.Run(context.Background(), Options{ApplyFixes: false, SkipUpdateCheck: true})

	for _, it := range report.Items {
		assert.Nil(t, it.FixResult)
	}
	_, err := os.Stat(missing)
	assert.True(t, os.IsNotExist(err))
}

func TestRun_skipUpdateCheckOmitsVersionItem(t *testing.T) {
	r := newMinimalDoctor(t)

	report := r.Run(context.Background(), Options{SkipUpdateCheck: true})

	for _, it := range report.Items {
		assert.NotEqual(t, "cli-version", it.Name, "cli-version should be omitted when SkipUpdateCheck=true")
	}
}

func TestRun_includesVersionByDefault(t *testing.T) {
	r := newMinimalDoctor(t)

	report := r.Run(context.Background(), Options{})

	var found bool
	for _, it := range report.Items {
		if it.Name == "cli-version" {
			found = true
		}
	}
	assert.True(t, found, "cli-version check should run by default")
}

// ──────────────────────────── auth-backend ────────────────────────────

func TestAuthBackendCheck_skippedWhenNoCreds(t *testing.T) {
	common.RegisterMultiplatformReader(nil) // keep test hermetic
	if _, ok := common.GetKeychainCredentials(); ok {
		t.Skip("dev keychain has creds — ResolveAuthConfig would succeed; can't exercise the skip branch here")
	}

	r := &Doctor{Envs: map[string]string{}}
	res := r.authBackendCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "skipped")
	assert.Contains(t, res.Detail, "source=none", "skip detail must surface the source for parity with non-skip output")
	assert.Contains(t, res.Detail, "BITRISE_BUILD_CACHE_AUTH_TOKEN", "skip detail must surface the underlying resolver error")
}

func TestAuthBackendCheck_okOnSuccessfulProbe(t *testing.T) {
	envs := map[string]string{
		common.EnvAuthToken:   "tok",
		common.EnvWorkspaceID: "ws-1",
	}

	r := &Doctor{
		Envs: envs,
		BackendProbe: func(_ context.Context, _ common.CacheAuthConfig, _ map[string]string) (time.Duration, error) {
			return 47 * time.Millisecond, nil
		},
	}

	res := r.authBackendCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "latency 47ms")
	assert.Contains(t, res.Detail, "ws-1")
}

func TestAuthBackendCheck_unauthenticatedIsError(t *testing.T) {
	envs := map[string]string{
		common.EnvAuthToken:   "tok",
		common.EnvWorkspaceID: "ws-1",
	}

	r := &Doctor{
		Envs: envs,
		BackendProbe: func(_ context.Context, _ common.CacheAuthConfig, _ map[string]string) (time.Duration, error) {
			return 30 * time.Millisecond, status.Error(codes.Unauthenticated, "bad token")
		},
	}

	res := r.authBackendCheck().Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
	assert.Contains(t, res.Detail, "auth-failed")
}

func TestAuthBackendCheck_permissionDeniedIsWorkspaceMisconfig(t *testing.T) {
	envs := map[string]string{
		common.EnvAuthToken:   "tok",
		common.EnvWorkspaceID: "ws-1",
	}

	r := &Doctor{
		Envs: envs,
		BackendProbe: func(_ context.Context, _ common.CacheAuthConfig, _ map[string]string) (time.Duration, error) {
			return 30 * time.Millisecond, status.Error(codes.PermissionDenied, "no access")
		},
	}

	res := r.authBackendCheck().Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
	assert.Contains(t, res.Detail, "workspace-misconfig")
}

func TestAuthBackendCheck_unavailableIsWarn(t *testing.T) {
	envs := map[string]string{
		common.EnvAuthToken:   "tok",
		common.EnvWorkspaceID: "ws-1",
	}

	r := &Doctor{
		Envs: envs,
		BackendProbe: func(_ context.Context, _ common.CacheAuthConfig, _ map[string]string) (time.Duration, error) {
			return 5 * time.Second, status.Error(codes.Unavailable, "connection refused")
		},
	}

	res := r.authBackendCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "network")
}

func TestRun_skipBackendProbeOmitsItem(t *testing.T) {
	r := newMinimalDoctor(t)

	report := r.Run(context.Background(), Options{SkipUpdateCheck: true, SkipBackendProbe: true})

	for _, it := range report.Items {
		assert.NotEqual(t, "auth-backend", it.Name, "auth-backend should be omitted when SkipBackendProbe=true")
	}
}

func TestBackendErrorState_kvSentinelUnauthenticated(t *testing.T) {
	assert.Equal(t, StateError, backendErrorState(kv.ErrCacheUnauthenticated))
}

func TestBackendErrorDetail_kvSentinelUnauthenticated(t *testing.T) {
	cfg := common.CacheAuthConfig{WorkspaceID: "ws-1"}
	got := backendErrorDetail(kv.ErrCacheUnauthenticated, cfg, common.AuthSourceKeychain, 30*time.Millisecond)
	assert.Contains(t, got, "auth-failed")
	assert.Contains(t, got, "source=keychain")
	assert.Contains(t, got, "ws-1")
}

func TestAuthBackendCheck_authFailureIsFixable(t *testing.T) {
	envs := map[string]string{common.EnvAuthToken: "tok", common.EnvWorkspaceID: "ws-1"}

	cases := []struct {
		name string
		err  error
	}{
		{"kv sentinel Unauthenticated", kv.ErrCacheUnauthenticated},
		{"status Unauthenticated", status.Error(codes.Unauthenticated, "bad token")},
		{"status PermissionDenied", status.Error(codes.PermissionDenied, "no access")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Doctor{
				Envs: envs,
				BackendProbe: func(_ context.Context, _ common.CacheAuthConfig, _ map[string]string) (time.Duration, error) {
					return 10 * time.Millisecond, tc.err
				},
			}

			res := r.authBackendCheck().Diagnose(context.Background())
			assert.True(t, res.Fixable, "auth-class failures must be marked Fixable so doctor --fix can offer the wizard")
		})
	}
}

func TestAuthBackendCheck_transientErrorNotFixable(t *testing.T) {
	envs := map[string]string{common.EnvAuthToken: "tok", common.EnvWorkspaceID: "ws-1"}

	r := &Doctor{
		Envs: envs,
		BackendProbe: func(_ context.Context, _ common.CacheAuthConfig, _ map[string]string) (time.Duration, error) {
			return 5 * time.Second, status.Error(codes.Unavailable, "connection refused")
		},
	}

	res := r.authBackendCheck().Diagnose(context.Background())
	assert.False(t, res.Fixable, "transport blips must not trigger a wizard re-launch")
}

func TestAuthPromptFixer_invokesInjectedPrompt(t *testing.T) {
	called := false
	f := AuthPromptFixer{Prompt: func() (string, string, error) {
		called = true

		return "ws-x", "tok-x", nil
	}}

	detail, err := f.Fix()
	require.NoError(t, err)
	assert.True(t, called)
	assert.Contains(t, detail, "ws-x")
}

func TestAuthPromptFixer_propagatesPromptError(t *testing.T) {
	f := AuthPromptFixer{Prompt: func() (string, string, error) { return "", "", errors.New("user aborted") }}

	_, err := f.Fix()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user aborted")
}

func TestProbeKey_lengthAndPrefix(t *testing.T) {
	k, err := probeKey()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(k, "doctor-probe-"), "got %q", k)
	assert.Len(t, k, len("doctor-probe-")+2*backendProbeKeyBytes, "8 hex chars expected for 4 random bytes")
}

// ──────────────────────────── daemon-up + update fixes ────────────────────────────

func TestXcelerateProxyCheck_fixerIsDaemonUpWhenNoPid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := &Doctor{
		ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Xcelerate: true} },
	}

	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	require.IsType(t, DaemonUpFixer{}, res.Fixer)
}

func TestCcacheHelperCheck_noSocketIsFixableViaDaemonUp(t *testing.T) {
	r := &Doctor{
		Envs:           map[string]string{"BITRISE_CCACHE_IPC_SOCKET_PATH": filepath.Join(t.TempDir(), "missing.sock")},
		ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Ccache: true} },
	}
	res := r.ccacheHelperCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.True(t, res.Fixable)
}

func TestCcacheHelperCheck_fixerIsDaemonUpWhenNoSocket(t *testing.T) {
	r := &Doctor{
		Envs:           map[string]string{"BITRISE_CCACHE_IPC_SOCKET_PATH": filepath.Join(t.TempDir(), "missing.sock")},
		ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Ccache: true} },
	}

	res := r.ccacheHelperCheck().Diagnose(context.Background())
	require.IsType(t, DaemonUpFixer{}, res.Fixer)
}

func TestCLIVersionCheck_behindIsFixable(t *testing.T) {
	r := &Doctor{
		CLIVersion:       "v2.8.3",
		HTTPClient:       &http.Client{},
		LatestReleaseTag: func(context.Context, *http.Client) (string, error) { return "v2.9.0", nil },
	}

	res := r.cliVersionCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.True(t, res.Fixable)
}

func TestCLIVersionCheck_localBuildIsNotFixable(t *testing.T) {
	r := &Doctor{CLIVersion: "devel"}

	res := r.cliVersionCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.False(t, res.Fixable, "local builds should not auto-upgrade")
}

func TestCLIVersionCheck_fixerIsUpdateFixer(t *testing.T) {
	r := &Doctor{
		CLIVersion:       "v2.8.3",
		HTTPClient:       &http.Client{},
		LatestReleaseTag: func(context.Context, *http.Client) (string, error) { return "v2.9.0", nil },
	}

	res := r.cliVersionCheck().Diagnose(context.Background())
	require.IsType(t, UpdateFixer{}, res.Fixer)
}

func TestXcelerateProxyCheck_socketDeadIsFixableViaRestart(t *testing.T) {
	home := t.TempDir()
	pidPath := xcelerateProxyPidPath(t, home)
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o755))
	require.NoError(t, os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600))

	r := &Doctor{
		Envs:           map[string]string{"BITRISE_XCELERATE_PROXY_SOCKET_PATH": filepath.Join(home, "missing.sock")},
		ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Xcelerate: true} },
	}

	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.True(t, res.Fixable)
}

func TestXcelerateProxyCheck_socketDeadFixerIsDaemonRestart(t *testing.T) {
	home := t.TempDir()
	pidPath := xcelerateProxyPidPath(t, home)
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o755))
	require.NoError(t, os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600))

	r := &Doctor{
		Envs:           map[string]string{"BITRISE_XCELERATE_PROXY_SOCKET_PATH": filepath.Join(home, "missing.sock")},
		ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Xcelerate: true} },
	}

	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	require.IsType(t, DaemonRestartFixer{}, res.Fixer)
}

func TestDaemonUpFix_propagatesError(t *testing.T) {
	f := DaemonUpFixer{Up: func(context.Context) ([]string, error) { return nil, errors.New("exit status 1") }}

	_, err := f.Fix()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 1")
}
