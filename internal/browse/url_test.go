//go:build unit

package browse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildURL_workspaceOnly_addsCIProviderUnknownFilter(t *testing.T) {
	got, err := BuildURL(BuildURLParams{
		WorkspaceID:      "ws_abc",
		CIProviderFilter: "unknown",
		BaseURL:          "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_abc/invocations?ci_provider=unknown", got)
}

func TestBuildURL_workspacePlusInvocationID_deepLinks(t *testing.T) {
	got, err := BuildURL(BuildURLParams{
		WorkspaceID:  "ws_abc",
		InvocationID: "inv_xyz",
		BaseURL:      "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_abc/invocations/inv_xyz", got)
}

func TestBuildURL_invocationIDIgnoresCIProviderFilter(t *testing.T) {
	got, err := BuildURL(BuildURLParams{
		WorkspaceID:      "ws_abc",
		InvocationID:     "inv_xyz",
		CIProviderFilter: "unknown",
		BaseURL:          "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.NotContains(t, got, "ci_provider=")
}

func TestBuildURL_noWorkspace_errors(t *testing.T) {
	_, err := BuildURL(BuildURLParams{BaseURL: "https://app.bitrise.io"})
	require.ErrorIs(t, err, ErrMissingWorkspace)
}

func TestBuildURL_defaultsBaseToConst(t *testing.T) {
	got, err := BuildURL(BuildURLParams{WorkspaceID: "ws_abc"})
	require.NoError(t, err)
	assert.Contains(t, got, "https://app.bitrise.io/build-cache/ws_abc/invocations")
}

func TestBuildURL_omitsCIProviderFilterWhenEmpty(t *testing.T) {
	got, err := BuildURL(BuildURLParams{
		WorkspaceID: "ws_abc",
		BaseURL:     "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_abc/invocations", got)
}

func TestBuildURL_badBaseURLIsError(t *testing.T) {
	_, err := BuildURL(BuildURLParams{
		WorkspaceID: "ws_abc",
		BaseURL:     "://not-a-url",
	})
	require.Error(t, err)
}

func TestBuildURL_pathEscapesSegments(t *testing.T) {
	got, err := BuildURL(BuildURLParams{
		WorkspaceID:  "ws/abc",
		InvocationID: "inv with space",
		BaseURL:      "https://app.bitrise.io",
	})
	require.NoError(t, err)

	assert.Equal(t, "https://app.bitrise.io/build-cache/ws%2Fabc/invocations/inv%20with%20space", got)
}
