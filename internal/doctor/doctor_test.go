//go:build unit

package doctor

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
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

func TestXcelerateProxyCheck_noPidIsWarn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	r := &Doctor{}
	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.False(t, res.Fixable)
}

func TestXcelerateProxyCheck_stalePidIsFixable(t *testing.T) {
	home := t.TempDir()
	pidPath := xcelerateProxyPidPath(t, home)
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o755))
	require.NoError(t, os.WriteFile(pidPath, []byte(strconv.Itoa(99999999)), 0o600))

	r := &Doctor{}
	res := r.xcelerateProxyCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.True(t, res.Fixable)
}

func TestXcelerateProxyCheck_fixRemovesStaleFile(t *testing.T) {
	home := t.TempDir()
	pidPath := xcelerateProxyPidPath(t, home)
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o755))
	require.NoError(t, os.WriteFile(pidPath, []byte("99999999"), 0o600))

	r := &Doctor{}
	detail, err := r.xcelerateProxyCheck().Fix()
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
	_, err := r.logDirsCheck().Fix()
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
