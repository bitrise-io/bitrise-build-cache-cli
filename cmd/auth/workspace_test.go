//go:build unit

package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/bitriseapi"
)

func workspacelessLogin(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(authpkg.EnvAuthToken, "")
	t.Setenv(authpkg.EnvWorkspaceID, "")
	t.Setenv(authpkg.EnvJWT, "")

	require.NoError(t, store.NewFile().Save(authpkg.TokenSet{AuthToken: "pat", RefreshToken: "refresh"}))
}

func organizationsAPI(t *testing.T, workspaces ...bitriseapi.Workspace) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/organizations", r.URL.Path)
		assert.Equal(t, "token pat", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": workspaces}))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("BITRISE_API_BASE_URL", srv.URL)
}

func runWorkspaceCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAuthWorkspaceCmd()

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return stdout.String(), err
}

// The two halves of the agent flow: list what the workspace-less login can see,
// then pin one — without a second sign-in.
func TestWorkspaceCmd_listThenSet(t *testing.T) {
	workspacelessLogin(t)
	organizationsAPI(t,
		bitriseapi.Workspace{Slug: "ws-one", Name: "One"},
		bitriseapi.Workspace{Slug: "ws-two", Name: "Two"},
	)

	out, err := runWorkspaceCmd(t, "--list")
	require.NoError(t, err)
	assert.Contains(t, out, "ws-one\tOne")
	assert.Contains(t, out, "ws-two\tTwo")

	_, err = runWorkspaceCmd(t, "--set", "ws-two")
	require.NoError(t, err)

	got, err := store.NewFile().Load()
	require.NoError(t, err)
	assert.Equal(t, "ws-two", got.WorkspaceID)
	assert.Equal(t, "refresh", got.RefreshToken, "the login must stay refreshable")

	out, err = runWorkspaceCmd(t, "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"workspace_id": "ws-two"`)
}
