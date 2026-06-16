//go:build unit

package common

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
)

type fakeKeychain struct {
	stored   keychain.Credentials
	hasStore bool
	loadErr  error
	saveErr  error
	saved    *keychain.Credentials
}

func (f *fakeKeychain) Load() (keychain.Credentials, error) {
	if f.loadErr != nil {
		return keychain.Credentials{}, f.loadErr
	}
	if !f.hasStore {
		return keychain.Credentials{}, keychain.ErrNotFound
	}

	return f.stored, nil
}

func (f *fakeKeychain) Save(c keychain.Credentials) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	cp := c
	f.saved = &cp

	return nil
}

func newTestPrompter(input string, secrets ...string) (*prompter, *bytes.Buffer) {
	out := &bytes.Buffer{}
	secretIdx := 0
	secretsCopy := append([]string{}, secrets...)

	return &prompter{
		reader: bufio.NewReader(strings.NewReader(input)),
		out:    out,
		readPassword: func() (string, error) {
			if secretIdx >= len(secretsCopy) {
				return "", nil
			}

			s := secretsCopy[secretIdx]
			secretIdx++

			return s, nil
		},
	}, out
}

func TestPromptTool_Selection(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  interactiveTool
	}{
		{name: "gradle", input: "1\n", want: toolGradle},
		{name: "bazel", input: "2\n", want: toolBazel},
		{name: "xcode", input: "3\n", want: toolXcode},
		{name: "ccache", input: "4\n", want: toolCcache},
		{name: "retry on invalid then accept", input: "abc\n0\n5\n2\n", want: toolBazel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestPrompter(tt.input)

			got, err := promptTool(p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPromptRequiredLine(t *testing.T) {
	t.Run("trims and returns first non-empty", func(t *testing.T) {
		p, _ := newTestPrompter("   \n  ws-123  \n")

		got, err := promptRequiredLine(p, "Workspace ID")
		require.NoError(t, err)
		assert.Equal(t, "ws-123", got)
	})

	t.Run("errors on closed stdin with no value", func(t *testing.T) {
		p, _ := newTestPrompter("")

		_, err := promptRequiredLine(p, "Workspace ID")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Workspace ID")
	})
}

func TestPromptRequiredSecret(t *testing.T) {
	t.Run("returns first non-empty masked value", func(t *testing.T) {
		p, _ := newTestPrompter("", "", "  secret-token  ")

		got, err := promptRequiredSecret(p, "Auth token")
		require.NoError(t, err)
		assert.Equal(t, "secret-token", got)
	})
}

func TestPromptPushEnabled(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "default (empty) is pull-only", input: "\n", want: false},
		{name: "explicit 1 is pull-only", input: "1\n", want: false},
		{name: "2 enables push", input: "2\n", want: true},
		{name: "retry on invalid then accept default", input: "abc\n\n", want: false},
		{name: "retry on invalid then choose push", input: "9\n2\n", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestPrompter(tt.input)

			got, err := promptPushEnabled(p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveCredentials_keychainPopulated_reusesSilently(t *testing.T) {
	t.Setenv(envAuthToken, "")
	t.Setenv(envWorkspaceID, "")

	kc := &fakeKeychain{
		hasStore: true,
		stored:   keychain.Credentials{AuthToken: "kc-tok", WorkspaceID: "kc-ws"},
	}
	p, out := newTestPrompter("")

	ws, tok, err := resolveCredentials(p, kc)
	require.NoError(t, err)
	assert.Equal(t, "kc-ws", ws)
	assert.Equal(t, "kc-tok", tok)
	assert.Contains(t, out.String(), "Reusing credentials")
	assert.Nil(t, kc.saved, "no Save should occur when keychain already has creds")
}

func TestResolveCredentials_envPresent_importsToKeychain(t *testing.T) {
	t.Setenv(envAuthToken, "env-tok")
	t.Setenv(envWorkspaceID, "env-ws")

	kc := &fakeKeychain{}
	p, out := newTestPrompter("")

	ws, tok, err := resolveCredentials(p, kc)
	require.NoError(t, err)
	assert.Equal(t, "env-ws", ws)
	assert.Equal(t, "env-tok", tok)
	require.NotNil(t, kc.saved, "env creds should be imported into keychain")
	assert.Equal(t, "env-tok", kc.saved.AuthToken)
	assert.Equal(t, "env-ws", kc.saved.WorkspaceID)
	assert.Contains(t, out.String(), "Importing")
}

func TestResolveCredentials_envPresent_saveErrorContinues(t *testing.T) {
	t.Setenv(envAuthToken, "env-tok")
	t.Setenv(envWorkspaceID, "env-ws")

	kc := &fakeKeychain{saveErr: errors.New("dbus down")}
	p, out := newTestPrompter("")

	ws, tok, err := resolveCredentials(p, kc)
	require.NoError(t, err, "save failures should not abort the wizard")
	assert.Equal(t, "env-ws", ws)
	assert.Equal(t, "env-tok", tok)
	assert.Contains(t, out.String(), "Continuing with env values for this run only")
}

func TestResolveCredentials_promptsAndPersists(t *testing.T) {
	t.Setenv(envAuthToken, "")
	t.Setenv(envWorkspaceID, "")

	kc := &fakeKeychain{}
	// workspace ID line + secret value via readPassword
	p, out := newTestPrompter("ws-prompted\n", "tok-prompted")

	ws, tok, err := resolveCredentials(p, kc)
	require.NoError(t, err)
	assert.Equal(t, "ws-prompted", ws)
	assert.Equal(t, "tok-prompted", tok)
	require.NotNil(t, kc.saved, "prompted creds should be persisted")
	assert.Equal(t, "tok-prompted", kc.saved.AuthToken)
	assert.Equal(t, "ws-prompted", kc.saved.WorkspaceID)
	assert.Contains(t, out.String(), "saved to the OS keychain")
}

func TestLoadStartingCredentials_keychainWins(t *testing.T) {
	kc := &fakeKeychain{
		hasStore: true,
		stored:   keychain.Credentials{AuthToken: "kc-tok", WorkspaceID: "kc-ws"},
	}
	ws, tok, src := loadStartingCredentials(kc, "env-ws", "env-tok")
	assert.Equal(t, credsSourceKeychain, src)
	assert.Equal(t, "kc-ws", ws)
	assert.Equal(t, "kc-tok", tok)
}

func TestLoadStartingCredentials_envWhenKeychainEmpty(t *testing.T) {
	ws, tok, src := loadStartingCredentials(&fakeKeychain{}, "env-ws", "env-tok")
	assert.Equal(t, credsSourceEnv, src)
	assert.Equal(t, "env-ws", ws)
	assert.Equal(t, "env-tok", tok)
}

func TestLoadStartingCredentials_noneWhenNothingSet(t *testing.T) {
	ws, tok, src := loadStartingCredentials(&fakeKeychain{}, "", "")
	assert.Equal(t, credsSourceNone, src)
	assert.Empty(t, ws)
	assert.Empty(t, tok)
}

func TestLoadStartingCredentials_partialKeychainTreatedAsMissing(t *testing.T) {
	// Token without workspace ID is incomplete; should fall through to env.
	kc := &fakeKeychain{hasStore: true, stored: keychain.Credentials{AuthToken: "kc-tok"}}
	ws, tok, src := loadStartingCredentials(kc, "env-ws", "env-tok")
	assert.Equal(t, credsSourceEnv, src)
	assert.Equal(t, "env-ws", ws)
	assert.Equal(t, "env-tok", tok)
}

func TestActivateCmd_HasInteractiveFlag(t *testing.T) {
	flag := ActivateCmd.Flags().Lookup("interactive")
	require.NotNil(t, flag, "--interactive flag should be registered on activate command")
	assert.Equal(t, "false", flag.DefValue)
}
