//go:build unit

package health

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

type fakeAuthLoader struct {
	creds keychain.Credentials
	err   error
}

func (f fakeAuthLoader) Load() (keychain.Credentials, error) { return f.creds, f.err }

func TestStatus_Overall(t *testing.T) {
	tests := []struct {
		name   string
		checks []Check
		want   State
	}{
		{"empty", nil, StateOK},
		{"all ok", []Check{{State: StateOK}, {State: StateOK}}, StateOK},
		{"warn beats ok", []Check{{State: StateOK}, {State: StateWarn}}, StateWarn},
		{"error beats warn", []Check{{State: StateWarn}, {State: StateError}, {State: StateOK}}, StateError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Status{Checks: tt.checks}.Overall())
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

func TestRun_xcelerateProxyMissingPidIsWarn(t *testing.T) {
	dir := t.TempDir() // empty: no proxy.pid → warn, not error

	r := &Runner{
		Envs:              map[string]string{},
		AuthLoader:        fakeAuthLoader{creds: keychain.Credentials{AuthToken: "t", WorkspaceID: "w"}},
		CLIVersion:        "devel",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "", nil },
		XcelerateProxyDir: func() string { return dir },
	}

	s := r.Run(context.Background())

	require.Len(t, s.Checks, 4)
	assert.Equal(t, "xcelerate-proxy", s.Checks[0].Name)
	assert.Equal(t, StateWarn, s.Checks[0].State, "no pid file should be warn, not error")
}

func TestRun_xcelerateProxyStalePidIsError(t *testing.T) {
	dir := t.TempDir()

	// PID 1 always exists on POSIX but signal-0 may succeed; pick an
	// implausibly-large PID instead so syscall.Kill returns ESRCH.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "proxy.pid"), []byte(strconv.Itoa(99999999)), 0o600))

	r := &Runner{
		Envs:              map[string]string{},
		AuthLoader:        fakeAuthLoader{creds: keychain.Credentials{AuthToken: "t", WorkspaceID: "w"}},
		CLIVersion:        "devel",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "", nil },
		XcelerateProxyDir: func() string { return dir },
	}

	s := r.Run(context.Background())
	assert.Equal(t, "xcelerate-proxy", s.Checks[0].Name)
	assert.Equal(t, StateError, s.Checks[0].State)
}

func TestRun_authFromKeychain(t *testing.T) {
	r := &Runner{
		Envs:              map[string]string{},
		AuthLoader:        fakeAuthLoader{creds: keychain.Credentials{AuthToken: "tok", WorkspaceID: "ws-keychain"}},
		CLIVersion:        "devel",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "", nil },
		XcelerateProxyDir: func() string { return t.TempDir() },
	}

	s := r.Run(context.Background())

	auth := s.Checks[2]
	assert.Equal(t, "auth", auth.Name)
	assert.Equal(t, StateOK, auth.State)
	assert.Contains(t, auth.Detail, "OS keychain")
	assert.Contains(t, auth.Detail, "ws-keychain")
}

func TestRun_authFromEnvWhenKeychainEmpty(t *testing.T) {
	r := &Runner{
		Envs: map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "env-tok",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "ws-env",
		},
		AuthLoader:        fakeAuthLoader{err: keychain.ErrNotFound},
		CLIVersion:        "devel",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "", nil },
		XcelerateProxyDir: func() string { return t.TempDir() },
	}

	auth := r.Run(context.Background()).Checks[2]
	assert.Equal(t, StateOK, auth.State)
	assert.Contains(t, auth.Detail, "environment variables")
	assert.Contains(t, auth.Detail, "ws-env")
}

func TestRun_authMissingIsError(t *testing.T) {
	r := &Runner{
		Envs:              map[string]string{},
		AuthLoader:        fakeAuthLoader{err: keychain.ErrNotFound},
		CLIVersion:        "devel",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "", nil },
		XcelerateProxyDir: func() string { return t.TempDir() },
	}

	auth := r.Run(context.Background()).Checks[2]
	assert.Equal(t, StateError, auth.State)
}

func TestRun_versionBehindLatestWarns(t *testing.T) {
	r := &Runner{
		Envs:              map[string]string{},
		AuthLoader:        fakeAuthLoader{creds: keychain.Credentials{AuthToken: "t", WorkspaceID: "w"}},
		CLIVersion:        "v2.8.0",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "v2.8.3", nil },
		XcelerateProxyDir: func() string { return t.TempDir() },
	}

	v := r.Run(context.Background()).Checks[3]
	assert.Equal(t, "cli-version", v.Name)
	assert.Equal(t, StateWarn, v.State)
	assert.Contains(t, v.Detail, "latest=v2.8.3")
}

func TestRun_versionLatestUnreachableWarns(t *testing.T) {
	r := &Runner{
		Envs:              map[string]string{},
		AuthLoader:        fakeAuthLoader{creds: keychain.Credentials{AuthToken: "t", WorkspaceID: "w"}},
		CLIVersion:        "v2.8.3",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "", errors.New("offline") },
		XcelerateProxyDir: func() string { return t.TempDir() },
	}

	v := r.Run(context.Background()).Checks[3]
	assert.Equal(t, StateWarn, v.State)
	assert.Contains(t, v.Detail, "could not check latest")
}

func TestRun_versionDirtyTreatedAsLocal(t *testing.T) {
	r := &Runner{
		Envs:              map[string]string{},
		AuthLoader:        fakeAuthLoader{creds: keychain.Credentials{AuthToken: "t", WorkspaceID: "w"}},
		CLIVersion:        "v2.8.4-0.xxx+dirty",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "v2.8.3", nil },
		XcelerateProxyDir: func() string { return t.TempDir() },
	}

	v := r.Run(context.Background()).Checks[3]
	assert.Equal(t, StateOK, v.State, "local builds shouldn't warn about being newer than a release")
	assert.Contains(t, v.Detail, "local build")
}

func TestRun_ccacheNoSocketIsWarn(t *testing.T) {
	r := &Runner{
		Envs:              map[string]string{"BITRISE_CCACHE_IPC_SOCKET_PATH": filepath.Join(t.TempDir(), "missing.sock")},
		AuthLoader:        fakeAuthLoader{creds: keychain.Credentials{AuthToken: "t", WorkspaceID: "w"}},
		CLIVersion:        "devel",
		HTTPClient:        &http.Client{},
		LatestReleaseTag:  func(context.Context, *http.Client) (string, error) { return "", nil },
		XcelerateProxyDir: func() string { return t.TempDir() },
	}

	cc := r.Run(context.Background()).Checks[1]
	assert.Equal(t, "ccache-helper", cc.Name)
	assert.Equal(t, StateWarn, cc.State)
}
