//go:build unit

package browse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildURL_workspaceOnly_addsSourceLocalFilter(t *testing.T) {
	got, err := BuildURL(BuildURLParams{
		WorkspaceID:  "ws_abc",
		SourceFilter: "local",
		BaseURL:      "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_abc/invocations?source=local", got)
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

func TestBuildURL_invocationIDIgnoresSourceFilter(t *testing.T) {
	// Source filter is a list-page concern; a deep link to a specific
	// invocation page doesn't need it. Confirm the param doesn't leak in.
	got, err := BuildURL(BuildURLParams{
		WorkspaceID:  "ws_abc",
		InvocationID: "inv_xyz",
		SourceFilter: "local",
		BaseURL:      "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.NotContains(t, got, "source=")
}

func TestBuildURL_noWorkspace_landsOnGenericPage(t *testing.T) {
	got, err := BuildURL(BuildURLParams{BaseURL: "https://app.bitrise.io"})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache", got)
}

func TestBuildURL_defaultsBaseToConst(t *testing.T) {
	got, err := BuildURL(BuildURLParams{WorkspaceID: "ws_abc"})
	require.NoError(t, err)
	assert.Contains(t, got, "https://app.bitrise.io/build-cache/ws_abc/invocations")
}

func TestBuildURL_omitsSourceFilterWhenEmpty(t *testing.T) {
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
