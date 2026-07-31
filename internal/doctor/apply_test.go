//go:build unit

package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubFixer struct {
	detail string
	err    error
	calls  *int
}

func (s stubFixer) Fix() (string, error) {
	*s.calls++

	return s.detail, s.err
}

// Diagnose must not touch the machine: the command shows the results and asks
// before anything is repaired.
func TestDiagnose_DoesNotFix(t *testing.T) {
	calls := 0
	d := &Doctor{
		CLIVersion:     "devel",
		ActivatedTools: nil,
		checksOverride: []Check{{
			Name: "thing",
			Diagnose: func(context.Context) Result {
				return Result{State: StateError, Detail: "broken", Fixable: true, Fixer: stubFixer{detail: "fixed", calls: &calls}}
			},
		}},
	}

	report := d.Diagnose(context.Background(), Options{})

	require.Len(t, report.Items, 1)
	assert.Zero(t, calls, "Diagnose must not run fixers")
	assert.Nil(t, report.Items[0].FixResult)
	assert.Equal(t, StateError, report.Overall)
}

func TestApplyFix_RecordsOutcome(t *testing.T) {
	calls := 0
	item := ReportItem{Name: "thing", Result: Result{
		State: StateError, Fixable: true, Fixer: stubFixer{detail: "repaired", calls: &calls},
	}}

	ApplyFix(&item)

	require.NotNil(t, item.FixResult)
	assert.Equal(t, "repaired", *item.FixResult)
	assert.Equal(t, StateOK, EffectiveState(item))
	assert.Equal(t, 1, calls)
}

func TestApplyFix_RecordsFailure(t *testing.T) {
	calls := 0
	item := ReportItem{Name: "thing", Result: Result{
		State: StateError, Fixable: true, Fixer: stubFixer{err: errors.New("nope"), calls: &calls},
	}}

	ApplyFix(&item)

	assert.Nil(t, item.FixResult)
	assert.Equal(t, "nope", item.FixError)
	assert.Equal(t, StateError, EffectiveState(item), "a failed fix must not read as healthy")
}

func TestApplyFix_NoFixerIsANoOp(t *testing.T) {
	item := ReportItem{Name: "thing", Result: Result{State: StateWarn, Detail: "meh"}}

	ApplyFix(&item)

	assert.Nil(t, item.FixResult)
	assert.Empty(t, item.FixError)
}

func TestFixable_OnlyItemsWithAFixer(t *testing.T) {
	calls := 0
	items := []ReportItem{
		{Name: "a", Result: Result{State: StateError, Fixer: stubFixer{calls: &calls}}},
		{Name: "b", Result: Result{State: StateWarn}},
		{Name: "c", Result: Result{State: StateWarn, Fixer: stubFixer{calls: &calls}}},
	}

	got := Fixable(items)

	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Name)
	assert.Equal(t, "c", got[1].Name, "check order is preserved")
}

// Run with ApplyFixes stays the scripted path: everything fixable is repaired.
func TestRun_WithApplyFixesStillFixesEverything(t *testing.T) {
	calls := 0
	d := &Doctor{
		CLIVersion: "devel",
		checksOverride: []Check{{
			Name: "thing",
			Diagnose: func(context.Context) Result {
				return Result{State: StateError, Fixable: true, Fixer: stubFixer{detail: "fixed", calls: &calls}}
			},
		}},
	}

	report := d.Run(context.Background(), Options{ApplyFixes: true})

	assert.Equal(t, 1, calls)
	require.NotNil(t, report.Items[0].FixResult)
	assert.Equal(t, StateOK, EffectiveOverall(report), "overall must reflect the applied fix")
}
