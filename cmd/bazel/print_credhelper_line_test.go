//go:build unit

package bazel

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintCredHelperLine_defaultDir(t *testing.T) {
	printCredHelperLineDir = ""

	var out bytes.Buffer
	printCredHelperLineCmd.SetOut(&out)
	require.NoError(t, printCredHelperLineCmd.RunE(printCredHelperLineCmd, nil))

	got := out.String()
	assert.Contains(t, got, "build --credential_helper=*.services.bitrise.io=%workspace%/tools/bitrise-build-cache-credhelper.sh")
	assert.True(t, len(got) > 0 && got[len(got)-1] == '\n', "snippet must end with newline")
}

func TestPrintCredHelperLine_customDir(t *testing.T) {
	printCredHelperLineDir = "vendor/bin"
	t.Cleanup(func() { printCredHelperLineDir = "" })

	var out bytes.Buffer
	printCredHelperLineCmd.SetOut(&out)
	require.NoError(t, printCredHelperLineCmd.RunE(printCredHelperLineCmd, nil))

	assert.Contains(t, out.String(), "%workspace%/vendor/bin/bitrise-build-cache-credhelper.sh")
}
