//go:build unit

package auth

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
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

func TestAuthSetCmd_preservesOAuthFieldsOnUsernameEdit(t *testing.T) {
	keyring.MockInit()
	kc := keychain.New()
	require.NoError(t, kc.Save(keychain.Credentials{
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
