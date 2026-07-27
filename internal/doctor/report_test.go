//go:build unit

package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterChecks_Only(t *testing.T) {
	all := []Check{{Name: "auth"}, {Name: "auth-backend"}, {Name: "ccache-binary"}}

	assert.Len(t, filterChecks(all, nil), 3, "empty Only runs everything")

	got := filterChecks(all, []string{"auth-backend", "auth", "nonexistent"})
	require.Len(t, got, 2)
	assert.Equal(t, "auth", got[0].Name, "check order is preserved, not the Only order")
	assert.Equal(t, "auth-backend", got[1].Name)
}

func TestChecks_XcodeSubsetIsRunnable(t *testing.T) {
	d := NewDoctor()
	d.ActivatedTools = nil

	for _, names := range [][]string{XcodeCheckNames, AuthProbeCheckNames} {
		checks := d.checks(Options{Only: names})

		got := make([]string, 0, len(checks))
		for _, c := range checks {
			got = append(got, c.Name)
			assert.NotNil(t, c.Diagnose, "%s must be diagnosable", c.Name)
		}
		assert.ElementsMatch(t, names, got, "every named check must exist")
	}
}

func TestIssueLines(t *testing.T) {
	fixed := "restarted"
	r := Report{Items: []ReportItem{
		{Name: "auth", Result: Result{State: StateOK, Detail: "fine"}},
		{Name: "xcelerate-proxy", Result: Result{State: StateWarn, Detail: "not running"}},
		{Name: "log-dirs", Result: Result{State: StateError, Detail: "not writable"}},
		{Name: "ccache-helper", Result: Result{State: StateWarn, Detail: "stuck"}, FixResult: &fixed},
	}}

	assert.Equal(t, []string{
		"xcelerate-proxy: not running",
		"log-dirs: not writable",
	}, IssueLines(r), "OK and fixed items are not issues")

	assert.Nil(t, IssueLines(Report{Items: []ReportItem{{Name: "auth", Result: Result{State: StateOK}}}}))
}

func TestEffectiveOverall(t *testing.T) {
	fixed := "done"
	warn := ReportItem{Name: "a", Result: Result{State: StateWarn}}
	errItem := ReportItem{Name: "b", Result: Result{State: StateError}}
	fixedErr := ReportItem{Name: "c", Result: Result{State: StateError}, FixResult: &fixed}

	assert.Equal(t, StateOK, EffectiveOverall(Report{Items: []ReportItem{fixedErr}}))
	assert.Equal(t, StateWarn, EffectiveOverall(Report{Items: []ReportItem{warn, fixedErr}}))
	assert.Equal(t, StateError, EffectiveOverall(Report{Items: []ReportItem{warn, errItem}}))
}

func TestXcodeCheckNames_ExcludesBackendProbe(t *testing.T) {
	assert.NotContains(t, XcodeCheckNames, "auth-backend")
	assert.Contains(t, AuthProbeCheckNames, "auth-backend")
}

func TestRun_OnlyRunsSelectedChecks(t *testing.T) {
	d := NewDoctor()
	d.ActivatedTools = nil

	report := d.Run(context.Background(), Options{
		Only:             []string{"log-dirs"},
		SkipUpdateCheck:  true,
		SkipBackendProbe: true,
	})

	require.Len(t, report.Items, 1)
	assert.Equal(t, "log-dirs", report.Items[0].Name)
}
