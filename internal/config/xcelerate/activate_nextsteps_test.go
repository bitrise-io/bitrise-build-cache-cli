//go:build unit

package xcelerate

import (
	"bytes"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
)

func TestPrintNextSteps_containsLoadBearingPhrases(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&buf))

	printNextSteps(logger)

	out := buf.String()
	assert.NotEmpty(t, out)
	assert.Contains(t, out, "source ~/.zshrc")
	assert.Contains(t, out, "not Xcode.app")
	assert.Contains(t, out, "bitrise-build-cache xcode build")
	assert.Contains(t, out, "docs/xcode-terminal-run.md")
	assert.Contains(t, out, "─")
}
