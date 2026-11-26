//go:build unit

package buildanalyzer

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTransitiveCommandDeps_LinearChain covers the simplest case: A reads
// Node N1 produced by B. B has no deps. A's upstream Commands = {B}.
func TestTransitiveCommandDeps_LinearChain(t *testing.T) {
	snap := Snapshot{
		Deps: map[string][]string{
			"CA":  {"NN1"},
			"NN1": {"CB"},
		},
	}

	got := TransitiveCommandDeps(snap, "CA")
	assert.ElementsMatch(t, []string{"CB"}, got)
}

// TestTransitiveCommandDeps_Diamond: A reads N1+N2; N1 produced by B, N2 by
// C; B reads N3 produced by D; C reads N4 produced by D. A's upstream =
// {B, C, D} — D must only appear once despite two paths.
func TestTransitiveCommandDeps_Diamond(t *testing.T) {
	snap := Snapshot{
		Deps: map[string][]string{
			"CA":  {"NN1", "NN2"},
			"NN1": {"CB"},
			"NN2": {"CC"},
			"CB":  {"NN3"},
			"NN3": {"CD"},
			"CC":  {"NN4"},
			"NN4": {"CD"},
		},
	}

	got := TransitiveCommandDeps(snap, "CA")
	sort.Strings(got)
	assert.Equal(t, []string{"CB", "CC", "CD"}, got, "diamond dedupes D")
}

// TestTransitiveCommandDeps_CycleGuard: pathological case where A reads a
// Node produced by A (shouldn't exist in a real llbuild graph, but defensive
// against a future schema or a corrupted DB). Walker must terminate and not
// include A itself.
func TestTransitiveCommandDeps_CycleGuard(t *testing.T) {
	snap := Snapshot{
		Deps: map[string][]string{
			"CA":  {"NN1"},
			"NN1": {"CA"},
		},
	}

	got := TransitiveCommandDeps(snap, "CA")
	assert.Empty(t, got, "self-loop walker terminates with no commands")
}

// TestTransitiveCommandDeps_NoDepsOrNilSnap covers the two zero-result paths:
// nil Snapshot.Deps and non-Command start key.
func TestTransitiveCommandDeps_NoDepsOrNilSnap(t *testing.T) {
	assert.Empty(t, TransitiveCommandDeps(Snapshot{}, "CA"))
	assert.Empty(t, TransitiveCommandDeps(Snapshot{Deps: map[string][]string{"CA": {"NN"}}}, "NN"),
		"Node key is never a valid starting point")
	assert.Empty(t, TransitiveCommandDeps(Snapshot{Deps: map[string][]string{"CA": {"NN"}}}, ""))
}

// TestUpstreamRanCommands_FiltersBySignatureIteration demonstrates the
// attribution split: upstream-but-skipped Commands don't count toward the
// ran-upstream set. Only Commands whose rule iteration equals the snapshot's
// current iteration count.
func TestUpstreamRanCommands_FiltersBySignatureIteration(t *testing.T) {
	snap := Snapshot{
		Iteration: 10,
		Rules: map[string]RuleResult{
			"CA": {Key: "CA", Iteration: 10},
			"CB": {Key: "CB", Iteration: 10}, // ran-this-build
			"CC": {Key: "CC", Iteration: 3},  // skipped-up-to-date
		},
		Deps: map[string][]string{
			"CA":  {"NN1", "NN2"},
			"NN1": {"CB"},
			"NN2": {"CC"},
		},
	}

	got := UpstreamRanCommands(snap, "CA")
	assert.ElementsMatch(t, []string{"CB"}, got, "only CB ran this build; CC is skipped")
}

// TestDirectCommandDeps_StopsAfterOneHop covers the one-hop contract:
// A → N1 → B → N2 → C must give only {B}, never C.
func TestDirectCommandDeps_StopsAfterOneHop(t *testing.T) {
	snap := Snapshot{
		Deps: map[string][]string{
			"CA":  {"NN1"},
			"NN1": {"CB"},
			"CB":  {"NN2"},
			"NN2": {"CC"},
		},
	}

	got := DirectCommandDeps(snap, "CA")
	assert.ElementsMatch(t, []string{"CB"}, got)
}
