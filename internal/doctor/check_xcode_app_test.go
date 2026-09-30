//go:build unit

package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeLaunchctlGetter struct {
	value string
	err   error
}

func (f fakeLaunchctlGetter) Getenv(_ context.Context, _ string) (string, error) {
	return f.value, f.err
}

func TestDiagnoseXcodeAppOverride_envAbsentReportsNotEnabled(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	plist := filepath.Join(tmp, "agent.plist")

	res := diagnoseXcodeAppOverride(override, plist, "", nil)

	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "not enabled")
}

func TestDiagnoseXcodeAppOverride_envSetButFileMissingWarns(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	plist := filepath.Join(tmp, "agent.plist")

	res := diagnoseXcodeAppOverride(override, plist, override, nil)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "missing")
}

func TestDiagnoseXcodeAppOverride_allHealthyReportsOK(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	plist := filepath.Join(tmp, "agent.plist")
	writeEmpty(t, override)
	writeEmpty(t, plist)

	res := diagnoseXcodeAppOverride(override, plist, override, nil)

	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "enabled")
	assert.Contains(t, res.Detail, override)
	assert.Contains(t, res.Detail, plist)
}

func TestDiagnoseXcodeAppOverride_launchctlErrorSurfacedAsWarn(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	plist := filepath.Join(tmp, "agent.plist")

	res := diagnoseXcodeAppOverride(override, plist, "", errors.New("boom"))

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "boom")
}

func TestDiagnoseXcodeAppOverride_envPointsElsewhereWarns(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "xcode-app.xcconfig")
	plist := filepath.Join(tmp, "agent.plist")
	writeEmpty(t, override)
	writeEmpty(t, plist)

	res := diagnoseXcodeAppOverride(override, plist, "/some/other/base.xcconfig", nil)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "/some/other/base.xcconfig")
	assert.Contains(t, res.Detail, "does not point at our override")
}

func writeEmpty(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
