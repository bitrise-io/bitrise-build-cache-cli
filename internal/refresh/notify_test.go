//go:build unit

package refresh

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNotify_silentWhenNoEntries(t *testing.T) {
	var buf bytes.Buffer
	Notify(&buf, "2.8.4", "2.8.5", nil)
	assert.Empty(t, buf.String())
}

func TestNotify_listsAllTools(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{
		{Tool: ToolBazel, ConfigPath: "/home/u/.bazelrc", CLIVersion: "2.8.4", RegisteredAt: time.Now()},
		{Tool: ToolGradle, ConfigPath: "/home/u/.gradle/init.d/x.kts", CLIVersion: "2.8.4", RegisteredAt: time.Now()},
	}

	Notify(&buf, "2.8.4", "2.8.5", entries)

	out := buf.String()
	assert.Contains(t, out, "bumped from 2.8.4 to 2.8.5")
	assert.Contains(t, out, "bitrise-build-cache activate bazel")
	assert.Contains(t, out, "bitrise-build-cache activate gradle")
	assert.Contains(t, out, "/home/u/.bazelrc")
	assert.Contains(t, out, "/home/u/.gradle/init.d/x.kts")
}

func TestActivateCommand_perTool(t *testing.T) {
	assert.Equal(t, "bitrise-build-cache activate gradle", activateCommand(ToolGradle))
	assert.Equal(t, "bitrise-build-cache activate bazel", activateCommand(ToolBazel))
	assert.Equal(t, "bitrise-build-cache activate xcode", activateCommand(ToolXcelerate))
	assert.Equal(t, "bitrise-build-cache activate c++", activateCommand(ToolCcache))
}

func TestOnBump_writesNudgeForRegisteredTools(t *testing.T) {
	home := t.TempDir()
	assert.NoError(t, Mark(home, ToolGradle, "/g", "2.8.4"))
	assert.NoError(t, Mark(home, ToolXcelerate, "/x", "2.8.4"))

	var buf bytes.Buffer
	err := OnBump(&buf, home, "2.8.4", "2.8.5")
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "activate gradle")
	assert.Contains(t, buf.String(), "activate xcode")
}

func TestOnBump_silentWhenRegistryEmpty(t *testing.T) {
	var buf bytes.Buffer
	err := OnBump(&buf, t.TempDir(), "2.8.4", "2.8.5")
	assert.NoError(t, err)
	assert.Empty(t, buf.String(), "no registered tools = no nudge")
}
