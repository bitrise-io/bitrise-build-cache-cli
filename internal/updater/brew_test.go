//go:build unit

package updater

import (
	"bytes"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
)

// loggerWithBuffer builds a project logger that writes into the supplied
// buffer — the standard test seam for asserting on updater output.
func loggerWithBuffer(buf *bytes.Buffer) log.Logger {
	return log.NewLogger(log.WithOutput(buf))
}

func TestPrintBrewUpgrade_includesFormulaAndUpgradeVerb(t *testing.T) {
	var buf bytes.Buffer
	PrintBrewUpgrade(loggerWithBuffer(&buf))

	out := buf.String()
	assert.Contains(t, out, "brew update")
	assert.Contains(t, out, "brew upgrade")
	assert.Contains(t, out, BrewFormula)
}
