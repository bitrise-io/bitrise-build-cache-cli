//go:build unit

package xcode_app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderSetenvAgent_includesLabelAndProgramArgs(t *testing.T) {
	got, err := RenderSetenvAgent("/Users/me/.bitrise-xcelerate/xcode-app.xcconfig")
	require.NoError(t, err)

	assert.Contains(t, got, "<string>"+SetenvAgentLabel+"</string>")
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

	// Raw special characters should not appear in the rendered body.
	assert.NotContains(t, got, "/Users/me/With <> & quote.xcconfig")
	// XML-escaped form should appear instead.
	assert.Contains(t, got, "&lt;")
	assert.Contains(t, got, "&gt;")
	assert.Contains(t, got, "&amp;")
}

func TestWriteAndRemoveSetenvAgent(t *testing.T) {
	home := t.TempDir()

	path, err := WriteSetenvAgent(home, "/Users/me/x.xcconfig")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, SetenvAgentPlistRelative), path)

	content, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(content), "<string>/Users/me/x.xcconfig</string>")

	removed, err := RemoveSetenvAgent(home)
	require.NoError(t, err)
	assert.Equal(t, path, removed)

	_, err = os.Stat(path)
	require.Error(t, err, "plist file should be gone after RemoveSetenvAgent")
}

func TestRemoveSetenvAgent_missingFileIsNotAnError(t *testing.T) {
	_, err := RemoveSetenvAgent(t.TempDir())
	require.NoError(t, err)
}
