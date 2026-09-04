//go:build unit

package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// 4096 < typical kernel pipe buffer (16-64KB); the Gradle init script reads stdout then stderr sequentially, a larger stderr could deadlock.
const pipeBufferSafeBound = 4096

func TestAuthTokenCmd_stdoutIsGradleFormat(t *testing.T) {
	cmd := authTokenCmd

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "raw-token")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "ws-123")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "")

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Equal(t, "ws-123:raw-token\n", stdout.String())
}

func TestAuthTokenCmd_workspaceFlagPicksPerWorkspaceEntry(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "")

	require.NoError(t, store.NewKeychain().Save(authpkg.TokenSet{
		AuthToken:   "machine-tok",
		WorkspaceID: "machine-ws",
		Workspaces: map[string]authpkg.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}))

	tokenWorkspace = "acme"
	t.Cleanup(func() { tokenWorkspace = "" })

	cmd := authTokenCmd
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Equal(t, "acme:acme-tok\n", stdout.String())
	assert.Empty(t, stderr.String(), "matching slug must not warn")
}

func TestAuthTokenCmd_workspaceFlagUnknownSlugFallsBackWithWarn(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "")

	require.NoError(t, store.NewKeychain().Save(authpkg.TokenSet{
		AuthToken:   "machine-tok",
		WorkspaceID: "machine-ws",
	}))

	tokenWorkspace = "missing"
	t.Cleanup(func() { tokenWorkspace = "" })

	cmd := authTokenCmd
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Equal(t, "machine-ws:machine-tok\n", stdout.String(), "unknown workspace must fall back to the machine-wide credential")
	assert.Contains(t, stderr.String(), "missing", "an unknown workspace must warn on stderr")
}

func TestAuthTokenCmd_stderrIsBoundedOnError(t *testing.T) {
	cmd := authTokenCmd

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "")

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Skip("dev machine has credentials configured; cannot exercise error path here")
	}

	require.Less(t, stderr.Len(), pipeBufferSafeBound,
		"auth token stderr must stay under %d bytes so the Gradle init script's sequential stdout+stderr drain can't deadlock", pipeBufferSafeBound)
	assert.NotEmpty(t, stderr.Bytes(), "error path must surface a one-line message on stderr")
}

func TestAuthUsernameCmd_stdoutIsBareResolvedName(t *testing.T) {
	cmd := authUsernameCmd
	t.Cleanup(func() { usernameSetValue = ""; cmd.Flags().Lookup("set").Changed = false })

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	t.Setenv("BITRISE_BUILD_CACHE_USERNAME", "alice")

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Equal(t, "alice\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestAuthUsernameCmd_jsonEmitsNameAndSource(t *testing.T) {
	cmd := authUsernameCmd
	usernameJSONOut = true
	t.Cleanup(func() { usernameJSONOut = false })

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	t.Setenv("BITRISE_BUILD_CACHE_USERNAME", "dave")

	require.NoError(t, cmd.RunE(cmd, nil))

	var got usernameOutput
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.Equal(t, "dave", got.Username)
	assert.Equal(t, "env", got.Source)
}

func TestAuthUsernameCmd_setPersistsIntoStoreHoldingCreds(t *testing.T) {
	keyring.MockInit()
	t.Setenv("BITRISE_BUILD_CACHE_USERNAME", "")

	// Seed keychain with token+workspace so it becomes the target store.
	require.NoError(t, keychain.New().Save(authpkg.TokenSet{AuthToken: "tok", WorkspaceID: "ws"}))

	envs := map[string]string{}
	require.NoError(t, setLocalUsername(envs, "carol"))

	creds, err := keychain.New().Load()
	require.NoError(t, err)
	assert.Equal(t, "carol", creds.Username)
	assert.Equal(t, "tok", creds.AuthToken, "token must survive a username-only set")
	assert.Equal(t, "ws", creds.WorkspaceID, "workspace must survive a username-only set")

	require.NoError(t, setLocalUsername(envs, ""))
	creds, err = keychain.New().Load()
	require.NoError(t, err)
	assert.Empty(t, creds.Username)
	assert.Equal(t, "tok", creds.AuthToken)
}

func TestScrubRawConfigAuthToken_stripsAuthAndKeepsRest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configPath := filepath.Join(configDir, "config.json")

	body := `{
  "proxyVersion": "1.2.3",
  "buildCacheEnabled": true,
  "authConfig": {
    "authToken": "secret-token-value",
    "workspaceID": "ws-123"
  }
}`
	require.NoError(t, os.WriteFile(configPath, []byte(body), 0o600))

	scrubbed, err := scrubRawConfigAuthToken(utils.DefaultOsProxy{}, configPath)
	require.NoError(t, err)
	assert.Equal(t, "~/.bitrise-xcelerate/config.json", scrubbed)

	rewritten, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(rewritten, &got))

	_, hasAuth := got["authConfig"]
	assert.False(t, hasAuth, "authConfig must be removed")
	assert.Equal(t, "1.2.3", got["proxyVersion"])
	assert.Equal(t, true, got["buildCacheEnabled"])
}

func TestScrubRawConfigAuthToken_noopWhenAlreadyClean(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configPath := filepath.Join(configDir, "config.json")

	require.NoError(t, os.WriteFile(configPath, []byte(`{"proxyVersion":"1"}`), 0o600))

	scrubbed, err := scrubRawConfigAuthToken(utils.DefaultOsProxy{}, configPath)
	require.NoError(t, err)
	assert.Empty(t, scrubbed)
}

func TestScrubGradleInitKts_blanksLiteralTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	initDir := filepath.Join(home, ".gradle", "init.d")
	require.NoError(t, os.MkdirAll(initDir, 0o755))
	initFile := filepath.Join(initDir, "bitrise-build-cache.init.gradle.kts")

	body := `settingsEvaluated {
    buildCache {
        remote(BitriseBuildCache::class.java) {
            endpoint = "https://cache.example"
            authToken = "secret-cache-token"
        }
    }
    rootProject {
        analytics {
            authToken.set("secret-analytics-token")
        }
    }
}
rootProject {
    rbe {
        authToken.set("secret-rbe-token")
    }
}
`
	require.NoError(t, os.WriteFile(initFile, []byte(body), 0o600))

	scrubbed, err := scrubGradleInitKts(utils.DefaultOsProxy{})
	require.NoError(t, err)
	assert.Equal(t, "~/.gradle/init.d/bitrise-build-cache.init.gradle.kts", scrubbed.path)
	assert.Contains(t, scrubbed.hint, "activate gradle")

	rewritten, err := os.ReadFile(initFile)
	require.NoError(t, err)

	got := string(rewritten)
	assert.NotContains(t, got, "secret-cache-token")
	assert.NotContains(t, got, "secret-analytics-token")
	assert.NotContains(t, got, "secret-rbe-token")
	assert.Contains(t, got, `authToken = ""`)
	assert.Contains(t, got, `authToken.set("")`)
	assert.Contains(t, got, `endpoint = "https://cache.example"`)
}

func TestScrubGradleInitKts_noopWhenValueSourceForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	initDir := filepath.Join(home, ".gradle", "init.d")
	require.NoError(t, os.MkdirAll(initDir, 0o755))
	initFile := filepath.Join(initDir, "bitrise-build-cache.init.gradle.kts")

	body := `authToken = providers.bitriseAuthToken()
authToken.set(providers.bitriseAuthToken())
`
	require.NoError(t, os.WriteFile(initFile, []byte(body), 0o600))

	scrubbed, err := scrubGradleInitKts(utils.DefaultOsProxy{})
	require.NoError(t, err)
	assert.Empty(t, scrubbed.path, "value-source form has no literal to scrub")
}

func TestAuthSetCmd_persistsUsernameToKeychain(t *testing.T) {
	keyring.MockInit()
	setToken = "tok-123"
	setWorkspaceID = "ws-456"
	setUsername = "alice"
	t.Cleanup(func() { setToken, setWorkspaceID, setUsername = "", "", "" })

	require.NoError(t, authSetCmd.RunE(authSetCmd, nil))

	creds, err := keychain.New().Load()
	require.NoError(t, err)
	assert.Equal(t, "tok-123", creds.AuthToken)
	assert.Equal(t, "ws-456", creds.WorkspaceID)
	assert.Equal(t, "alice", creds.Username)
}

func TestAuthSetCmd_emptyUsernameLeavesFieldEmpty(t *testing.T) {
	keyring.MockInit()
	setToken = "tok"
	setWorkspaceID = "ws"
	setUsername = ""
	t.Cleanup(func() { setToken, setWorkspaceID, setUsername = "", "", "" })

	require.NoError(t, authSetCmd.RunE(authSetCmd, nil))

	creds, err := keychain.New().Load()
	require.NoError(t, err)
	assert.Empty(t, creds.Username)
}

func TestAuthSetCmd_storageFileWritesToMultiplatformConfig(t *testing.T) {
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)

	setToken = "tok-file"
	setWorkspaceID = "ws-file"
	setUsername = "bob"
	setStorage = "file"
	t.Cleanup(func() { setToken, setWorkspaceID, setUsername, setStorage = "", "", "", "" })

	require.NoError(t, authSetCmd.RunE(authSetCmd, nil))

	creds, ok := multiplatformconfig.ReadCredentials(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.True(t, ok, "credentials must be present in multiplatform config after --storage=file")
	assert.Equal(t, "tok-file", creds.AuthToken)
	assert.Equal(t, "ws-file", creds.WorkspaceID)
	assert.Equal(t, "bob", creds.Username)

	mp, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.NoError(t, err)
	assert.Equal(t, "tok-file", mp.AuthConfig.AuthToken, "AuthConfig must mirror for legacy reactnative/invocation readers")
	assert.Equal(t, "ws-file", mp.AuthConfig.WorkspaceID)

	_, err = keychain.New().Load()
	assert.ErrorIs(t, err, keychain.ErrNotFound)
}

func TestAuthSetCmd_ciDetectionRoutesToFile(t *testing.T) {
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CIRCLECI", "true")

	setToken = "tok-ci"
	setWorkspaceID = "ws-ci"
	setStorage = "" // auto
	t.Cleanup(func() { setToken, setWorkspaceID, setStorage = "", "", "" })

	require.NoError(t, authSetCmd.RunE(authSetCmd, nil))

	creds, ok := multiplatformconfig.ReadCredentials(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.True(t, ok)
	assert.Equal(t, "tok-ci", creds.AuthToken)

	_, err := keychain.New().Load()
	assert.ErrorIs(t, err, keychain.ErrNotFound)
}

func TestAuthSetCmd_preservesOAuthFieldsOnUsernameEdit(t *testing.T) {
	keyring.MockInit()
	kc := keychain.New()
	require.NoError(t, kc.Save(authpkg.TokenSet{
		AuthToken:    "old-tok",
		WorkspaceID:  "old-ws",
		RefreshToken: "refresh-abc",
		JWT:          "jwt-xyz",
	}))

	setToken = "old-tok"
	setWorkspaceID = "old-ws"
	setUsername = "alice"
	t.Cleanup(func() { setToken, setWorkspaceID, setUsername = "", "", "" })

	require.NoError(t, authSetCmd.RunE(authSetCmd, nil))

	creds, err := kc.Load()
	require.NoError(t, err)
	assert.Equal(t, "alice", creds.Username)
	assert.Equal(t, "refresh-abc", creds.RefreshToken, "OAuth refresh token must survive auth set --username")
	assert.Equal(t, "jwt-xyz", creds.JWT)
}

// The keychain-unavailable case is not cosmetic: aborting on it left a Linux
// host with no way to remove the credentials it actually has, since the file
// store is only reached after the keychain.
func TestClearTargets_UnavailableKeychainDoesNotBlockTheFileStore(t *testing.T) {
	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	file := &fakeStore{backend: authpkg.BackendFile}
	err := clearTargets(logger, []store.Store{
		&fakeStore{backend: authpkg.BackendKeychain, clearErr: errors.Join(keychain.ErrUnavailable, errors.New("no secret service"))},
		file,
	})

	require.NoError(t, err)
	assert.True(t, file.cleared, "the file store must still be cleared")
	assert.Contains(t, out.String(), "Skipped the OS keychain")
}

func TestClearTargets_RealFailureIsReported(t *testing.T) {
	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	file := &fakeStore{backend: authpkg.BackendFile}
	err := clearTargets(logger, []store.Store{
		&fakeStore{backend: authpkg.BackendKeychain, clearErr: errors.New("keychain is locked")},
		file,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keychain is locked")
	assert.True(t, file.cleared, "one backend failing must not skip the other")
}

type fakeStore struct {
	backend  authpkg.Backend
	clearErr error
	cleared  bool
}

func (s *fakeStore) Backend() authpkg.Backend { return s.backend }
func (s *fakeStore) Load() (authpkg.TokenSet, error) {
	return authpkg.TokenSet{}, store.ErrNotFound
}
func (s *fakeStore) Save(authpkg.TokenSet) error { return nil }
func (s *fakeStore) Clear() error {
	if s.clearErr != nil {
		return s.clearErr
	}
	s.cleared = true

	return nil
}
