//go:build unit

package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
)

type noopFixer struct{}

func (noopFixer) Fix() (string, error) { return "done", nil }

func item(name string, state doctorpkg.State, detail string, fixable bool) doctorpkg.ReportItem {
	res := doctorpkg.Result{State: state, Detail: detail}
	if fixable {
		res.Fixable = true
		res.Fixer = noopFixer{}
	}

	return doctorpkg.ReportItem{Name: name, Result: res}
}

// Errors are what break a build, so they start ticked; warnings are offered but
// the user has to opt into them.
func TestFixOptions_PreselectsErrorsOnly(t *testing.T) {
	fixable := []doctorpkg.ReportItem{
		item("auth", doctorpkg.StateError, "no credentials found", true),
		item("log-dirs", doctorpkg.StateWarn, "missing", true),
		item("xcelerate-proxy", doctorpkg.StateError, "not running", true),
	}

	options, selected := fixOptions(fixable, palette(false))

	require.Len(t, options, 3, "every fixable issue must be offered")
	assert.Equal(t, []string{"auth", "xcelerate-proxy"}, selected)
	assert.NotContains(t, selected, "log-dirs", "a warning must not be repaired unasked")
}

func TestFixOptions_LabelsCarryStateAndDetail(t *testing.T) {
	options, _ := fixOptions([]doctorpkg.ReportItem{
		item("auth", doctorpkg.StateError, "no credentials found", true),
	}, palette(false))

	require.Len(t, options, 1)
	assert.Contains(t, options[0].Key, "auth")
	assert.Contains(t, options[0].Key, "no credentials found", "the label should say what is wrong")
	assert.Equal(t, "auth", options[0].Value, "the value is the check name the caller matches on")
}

// An already-fixed item reads as OK, so it must not be preselected on a rerun.
func TestFixOptions_FixedItemIsNotPreselected(t *testing.T) {
	fixed := "repaired"
	it := item("auth", doctorpkg.StateError, "no credentials found", true)
	it.FixResult = &fixed

	_, selected := fixOptions([]doctorpkg.ReportItem{it}, palette(false))

	assert.Empty(t, selected)
}

func TestFixOptions_Empty(t *testing.T) {
	options, selected := fixOptions(nil, palette(false))
	assert.Empty(t, options)
	assert.Empty(t, selected)
}
