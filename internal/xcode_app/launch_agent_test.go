//go:build unit

package xcode_app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

func TestRenderSetenvAgent_includesLabelAndProgramArgs(t *testing.T) {
	got, err := RenderSetenvAgent("/Users/me/.bitrise-xcelerate/xcode-app.xcconfig")
	require.NoError(t, err)

	assert.Contains(t, got, "<string>"+paths.XcodeAppSetenvAgentLabel+"</string>")
	assert.Contains(t, got, "<string>"+LaunchctlBin+"</string>")
	assert.Contains(t, got, "<string>setenv</string>")
	assert.Contains(t, got, "<string>"+XCConfigEnvVar+"</string>")
	assert.Contains(t, got, "<string>/Users/me/.bitrise-xcelerate/xcode-app.xcconfig</string>")
	assert.Contains(t, got, "<key>RunAtLoad</key>")
	assert.Contains(t, got, "<true/>")
}

func TestRenderSetenvAgent_emptyPathIsError(t *testing.T) {
	_, err := RenderSetenvAgent("")
	require.Error(t, err)
}

func TestRenderSetenvAgent_escapesXMLSpecialChars(t *testing.T) {
	got, err := RenderSetenvAgent("/Users/me/With <> & quote.xcconfig")
	require.NoError(t, err)

	assert.NotContains(t, got, "/Users/me/With <> & quote.xcconfig")
	assert.Contains(t, got, "&lt;")
	assert.Contains(t, got, "&gt;")
	assert.Contains(t, got, "&amp;")
}

func TestWriteAndRemoveSetenvAgent(t *testing.T) {
	home := t.TempDir()
	osProxy := utils.DefaultOsProxy{}

	path, err := WriteSetenvAgent(osProxy, home, "/Users/me/x.xcconfig")
	require.NoError(t, err)
	assert.Equal(t, paths.FromHome(home).XcodeAppSetenvAgentPlistFile(), path)

	content, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(content), "<string>/Users/me/x.xcconfig</string>")

	removed, err := RemoveSetenvAgent(osProxy, home)
	require.NoError(t, err)
	assert.Equal(t, path, removed)

	_, err = os.Stat(path)
	require.Error(t, err, "plist file should be gone after RemoveSetenvAgent")
}

func TestRemoveSetenvAgent_missingFileIsNotAnError(t *testing.T) {
	_, err := RemoveSetenvAgent(utils.DefaultOsProxy{}, t.TempDir())
	require.NoError(t, err)
}
