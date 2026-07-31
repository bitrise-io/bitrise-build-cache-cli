//go:build unit

package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

func TestEffectiveOverall(t *testing.T) {
	fixed := "done"
	warn := ReportItem{Name: "a", Result: Result{State: StateWarn}}
	errItem := ReportItem{Name: "b", Result: Result{State: StateError}}
	fixedErr := ReportItem{Name: "c", Result: Result{State: StateError}, FixResult: &fixed}

	assert.Equal(t, StateOK, EffectiveOverall(Report{Items: []ReportItem{fixedErr}}))
	assert.Equal(t, StateWarn, EffectiveOverall(Report{Items: []ReportItem{warn, fixedErr}}))
	assert.Equal(t, StateError, EffectiveOverall(Report{Items: []ReportItem{warn, errItem}}))
}

func TestStateDirCandidates_ScopedToActivatedTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := paths.FromHome(home)

	activated := func(tools ...toolconfig.Tool) func() map[toolconfig.Tool]bool {
		return func() map[toolconfig.Tool]bool {
			out := map[toolconfig.Tool]bool{}
			for _, tool := range tools {
				out[tool] = true
			}

			return out
		}
	}

	d := &Doctor{ActivatedTools: activated(toolconfig.Xcelerate)}
	assert.Equal(t, []string{p.XcelerateLogDir()}, d.stateDirCandidates(),
		"an Xcode-only setup must not be asked about ccache's log dir")

	d = &Doctor{ActivatedTools: activated(toolconfig.Ccache)}
	assert.Equal(t, []string{p.CcacheLogDir()}, d.stateDirCandidates())

	d = &Doctor{ActivatedTools: activated(toolconfig.Xcelerate, toolconfig.Ccache)}
	assert.Equal(t, []string{p.XcelerateLogDir(), p.CcacheLogDir()}, d.stateDirCandidates())

	d = &Doctor{ActivatedTools: activated(toolconfig.Gradle)}
	assert.Empty(t, d.stateDirCandidates(), "Gradle writes no CLI-managed logs")

	res := (&Doctor{ActivatedTools: activated(toolconfig.Gradle)}).logDirsCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.False(t, res.Fixable)

	explicit := []string{"/tmp/injected"}
	assert.Equal(t, explicit, (&Doctor{StateDirCandidates: explicit}).stateDirCandidates(),
		"an explicit list still wins")
}
