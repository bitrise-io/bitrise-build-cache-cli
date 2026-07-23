//go:build unit

package common

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpiryAwareResolver_NonOAuthSource_UsesPlainResolve(t *testing.T) {
	envCfg := CacheAuthConfig{AuthToken: "env-tok", WorkspaceID: "env-ws"}
	resolve := func(_ map[string]string) (CacheAuthConfig, AuthSource, error) {
		return envCfg, AuthSourceEnvVars, nil
	}
	refresh := func(_ context.Context) (string, string, error) {
		t.Fatal("refreshFn must not be called when source is not keychain")

		return "", "", nil
	}

	r := newExpiryAwareResolver(context.Background(), map[string]string{}, refresh, resolve, log.NewLogger())

	assert.Equal(t, envCfg, r.Get())
}

func TestExpiryAwareResolver_OAuthSource_UsesRefreshFn(t *testing.T) {
	storedCfg := CacheAuthConfig{AuthToken: "stored-tok", WorkspaceID: "stored-ws"}
	resolve := func(_ map[string]string) (CacheAuthConfig, AuthSource, error) {
		return storedCfg, AuthSourceKeychain, nil
	}
	refresh := func(_ context.Context) (string, string, error) {
		return "new-pat", "new-wsid", nil
	}

	r := newExpiryAwareResolver(context.Background(), map[string]string{}, refresh, resolve, log.NewLogger())

	got := r.Get()

	assert.Equal(t, "new-pat", got.AuthToken)
	assert.Equal(t, "new-wsid", got.WorkspaceID)
}

func TestExpiryAwareResolver_RefreshFnError_FallsBackToPlainResolve(t *testing.T) {
	storedCfg := CacheAuthConfig{AuthToken: "stored-tok", WorkspaceID: "stored-ws"}
	resolve := func(_ map[string]string) (CacheAuthConfig, AuthSource, error) {
		return storedCfg, AuthSourceKeychain, nil
	}
	refresh := func(_ context.Context) (string, string, error) {
		return "", "", errors.New("token refresh failed")
	}

	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	r := newExpiryAwareResolver(context.Background(), map[string]string{}, refresh, resolve, logger)

	got := r.Get()

	assert.Equal(t, storedCfg, got)
	assert.Contains(t, out.String(), "refreshFn failed")
}

func TestExpiryAwareResolver_NilRefreshFn_FallsThrough(t *testing.T) {
	storedCfg := CacheAuthConfig{AuthToken: "stored-tok", WorkspaceID: "stored-ws"}
	resolve := func(_ map[string]string) (CacheAuthConfig, AuthSource, error) {
		return storedCfg, AuthSourceKeychain, nil
	}

	r := newExpiryAwareResolver(context.Background(), map[string]string{}, nil, resolve, log.NewLogger())

	assert.Equal(t, storedCfg, r.Get())
}

func TestExpiryAwareResolver_ResolveError_ReturnsCfgAndLogsWarn(t *testing.T) {
	resolve := func(_ map[string]string) (CacheAuthConfig, AuthSource, error) {
		return CacheAuthConfig{}, AuthSourceNone, errors.New("no creds")
	}
	refresh := func(_ context.Context) (string, string, error) {
		t.Fatal("refreshFn must not be called after resolve error")

		return "", "", nil
	}

	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	r := newExpiryAwareResolver(context.Background(), map[string]string{}, refresh, resolve, logger)

	got := r.Get()

	assert.Equal(t, CacheAuthConfig{}, got)
	assert.Contains(t, out.String(), "ResolveAuthConfig failed")
}

func TestNewExpiryAwareResolver_UsesDefaultResolve(t *testing.T) {
	envs := map[string]string{
		EnvAuthToken:   "public-tok",
		EnvWorkspaceID: "public-ws",
	}

	r := NewExpiryAwareResolver(context.Background(), envs, nil, log.NewLogger())

	require.NotNil(t, r)
	got := r.Get()

	assert.Equal(t, "public-tok", got.AuthToken)
	assert.Equal(t, "public-ws", got.WorkspaceID)
}
