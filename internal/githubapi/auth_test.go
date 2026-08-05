//go:build unit

package githubapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetAuthHeader_noEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	SetAuthHeader(req)

	assert.Empty(t, req.Header.Get("Authorization"))
}

func TestSetAuthHeader_githubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_primary")
	t.Setenv("GH_TOKEN", "ghp_fallback")

	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	SetAuthHeader(req)

	assert.Equal(t, "Bearer ghp_primary", req.Header.Get("Authorization"))
}

func TestSetAuthHeader_ghTokenFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "ghp_fallback")

	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	SetAuthHeader(req)

	assert.Equal(t, "Bearer ghp_fallback", req.Header.Get("Authorization"))
}
