//go:build unit

package daemon

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePlist_xcelerateProxy(t *testing.T) {
	paths := NewPathsFromHome("/Users/alice")
	svc := Service{Name: "xcelerate-proxy", Args: []string{"xcelerate", "start-proxy"}}

	got, err := GeneratePlist(svc, "/usr/local/bin/bitrise-build-cache", paths)
	require.NoError(t, err)

	assert.Contains(t, got, "<string>io.bitrise.build-cache.xcelerate-proxy</string>")
	assert.Contains(t, got, "<string>/usr/local/bin/bitrise-build-cache</string>")
	assert.Contains(t, got, "<string>xcelerate</string>")
	assert.Contains(t, got, "<string>start-proxy</string>")
	assert.Contains(t, got, "<string>/Users/alice/.local/state/bitrise-build-cache/logs/xcelerate-proxy.out.log</string>")
	assert.Contains(t, got, "<string>/Users/alice/.local/state/bitrise-build-cache/logs/xcelerate-proxy.err.log</string>")
	assert.Contains(t, got, "<key>KeepAlive</key>")
	assert.Contains(t, got, "<key>RunAtLoad</key>")
	assert.Contains(t, got, "<string>Background</string>")
}

func TestGeneratePlist_ccacheHelper(t *testing.T) {
	paths := NewPathsFromHome("/home/build")
	svc := Service{Name: "ccache-helper", Args: []string{"ccache", "storage-helper", "start"}}

	got, err := GeneratePlist(svc, "/opt/cli/bitrise-build-cache", paths)
	require.NoError(t, err)

	assert.Contains(t, got, "<string>io.bitrise.build-cache.ccache-helper</string>")
	assert.Contains(t, got, "<string>storage-helper</string>")
	// Argv order preserved: executable first, then args.
	exeIdx := strings.Index(got, "/opt/cli/bitrise-build-cache")
	argIdx := strings.Index(got, "<string>ccache</string>")
	require.Positive(t, exeIdx)
	require.Positive(t, argIdx)
	assert.Less(t, exeIdx, argIdx, "executable must precede argv in ProgramArguments")
}

func TestGeneratePlist_escapesXML(t *testing.T) {
	paths := NewPathsFromHome(`/Users/alice & friends`)
	svc := Service{Name: "ccache-helper", Args: []string{"ccache", `arg<with>weird&chars`}}

	got, err := GeneratePlist(svc, `/usr/local/bin/bitrise-build-cache`, paths)
	require.NoError(t, err)

	// Raw ampersand must be encoded.
	assert.NotContains(t, got, " & friends")
	assert.Contains(t, got, "&amp; friends")
	assert.Contains(t, got, "arg&lt;with&gt;weird&amp;chars")
}

func TestGeneratePlist_emptyExecutableErrors(t *testing.T) {
	_, err := GeneratePlist(Service{Name: "x"}, "", NewPathsFromHome("/tmp"))
	require.Error(t, err)
}
