//go:build unit

package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestDiagnoseXcodeAppOverride_overrideMissingWarns(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	socket := filepath.Join(tmp, "xcelerate-proxy.sock")

	res := diagnoseXcodeAppOverride(override, socket, utils.DefaultOsProxy{})

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "override xcconfig missing")
	assert.Contains(t, res.Detail, override)
}

func TestDiagnoseXcodeAppOverride_overridePresentSocketLiveReportsOK(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	socket := filepath.Join(tmp, "xcelerate-proxy.sock")
	writeEmpty(t, override)
	writeEmpty(t, socket)

	res := diagnoseXcodeAppOverride(override, socket, utils.DefaultOsProxy{})

	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, override)
	assert.Contains(t, res.Detail, socket)
}

func TestDiagnoseXcodeAppOverride_overridePresentButSocketMissingWarns(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	socket := filepath.Join(tmp, "xcelerate-proxy.sock")
	writeEmpty(t, override) // socket deliberately absent

	res := diagnoseXcodeAppOverride(override, socket, utils.DefaultOsProxy{})

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "proxy socket")
	assert.Contains(t, res.Detail, "not live")
}

func TestDiagnoseXcodeAppOverride_bothMissingPrefersOverrideWarn(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	socket := filepath.Join(tmp, "xcelerate-proxy.sock")

	res := diagnoseXcodeAppOverride(override, socket, utils.DefaultOsProxy{})

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "override xcconfig missing")
}

func writeEmpty(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
