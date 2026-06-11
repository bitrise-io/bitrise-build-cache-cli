//go:build unit

package updater

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrintBrewUpgrade_includesFormulaAndUpgradeVerb(t *testing.T) {
	var buf bytes.Buffer
	PrintBrewUpgrade(&buf)

	out := buf.String()
	assert.Contains(t, out, "brew update")
	assert.Contains(t, out, "brew upgrade")
	assert.Contains(t, out, BrewFormula)
}
