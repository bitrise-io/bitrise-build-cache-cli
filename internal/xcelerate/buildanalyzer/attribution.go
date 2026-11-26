package buildanalyzer

// Upstream-attribution walks across Snapshot.Deps.
//
// llbuild's rule graph has two kinds of nodes: Commands (manifest-derived,
// key-prefixed `C`) and Nodes (files / virtual nodes, key-prefixed `N`).
// Every edge carries through a Node — a Command reads a Node (an input file),
// a Node is produced by a Command (an output file). So the "A depends on B"
// relation at the Command level is a two-hop walk: `C_A -> N_X -> C_B`.
//
// The walkers here return Command keys only; Node intermediates are stepped
// over. All walkers are iterative DFS so the call stack doesn't blow on the
// 10k+ edge chains a monorepo produces. Cycle guards use a visited set.

// Rule-key prefix tags.
const (
	rulePrefixCommand = 'C'
	rulePrefixNode    = 'N'
)

// TransitiveCommandDeps returns every Command key transitively reachable from
// cmdKey via the two-hop C -> N -> C walk. The starting Command is NOT
// included in the result. Order is insertion-order along a DFS pre-order
// traversal — stable between runs on the same adjacency, not sorted.
//
// A plain []string reads cleaner here than iter.Seq — callers typically want
// `len()` and random access (for truncation / amplification math), and the
// result set is bounded by the total Command count which fits comfortably in
// RAM.
func TransitiveCommandDeps(snap Snapshot, cmdKey string) []string {
	if snap.Deps == nil {
		return nil
	}

	if !isCommandKey(cmdKey) {
		return nil
	}

	visited := make(map[string]struct{}, 32)
	visited[cmdKey] = struct{}{}

	out := make([]string, 0, 32)
	stack := []string{cmdKey}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		for _, cmd := range directCommandDeps(snap, current, visited) {
			visited[cmd] = struct{}{}
			out = append(out, cmd)
			stack = append(stack, cmd)
		}
	}

	return out
}

// DirectCommandDeps returns the immediate upstream Commands of cmdKey — the
// `C -> N -> C` single-hop. Used by Classify to populate ChangeCauseKeys. The
// walker still uses the Command-level visited set so a Node fanning into
// multiple producers deduplicates.
func DirectCommandDeps(snap Snapshot, cmdKey string) []string {
	if snap.Deps == nil || !isCommandKey(cmdKey) {
		return nil
	}

	return directCommandDeps(snap, cmdKey, map[string]struct{}{cmdKey: {}})
}

// UpstreamRanCommands returns the subset of TransitiveCommandDeps whose rule
// row carries the snapshot's current iteration — i.e. Commands that also ran
// this build. Used to compute UpstreamRanCount and RootCause: when the count
// is zero the task is a root cause of the ran set.
//
// Returns a deduplicated set as a []string in DFS pre-order; callers typically
// treat it as a set via `len()`.
func UpstreamRanCommands(snap Snapshot, cmdKey string) []string {
	all := TransitiveCommandDeps(snap, cmdKey)
	if len(all) == 0 {
		return nil
	}

	out := make([]string, 0, len(all))
	for _, k := range all {
		rule, ok := snap.Rules[k]
		if !ok {
			continue
		}

		if ruleRanThisBuild(rule, snap.Iteration) {
			out = append(out, k)
		}
	}

	return out
}

// Private — helpers.

// directCommandDeps walks `cmdKey`'s declared upstream dep set once: each dep
// is either a Node (step through to its producers) or a Command (collect). We
// never emit `cmdKey` itself, and the shared `visited` set stops a diamond
// from double-counting a shared ancestor. A Command reached through two
// different Node intermediates (e.g. a `.o` and a `.d` both produced by the
// same clang invocation) is emitted once.
func directCommandDeps(snap Snapshot, cmdKey string, visited map[string]struct{}) []string {
	nodeDeps := snap.Deps[cmdKey]
	if len(nodeDeps) == 0 {
		return nil
	}

	localSeen := make(map[string]struct{}, len(nodeDeps))
	out := make([]string, 0, len(nodeDeps))

	emit := func(cmd string) {
		if _, seen := visited[cmd]; seen {
			return
		}

		if _, seen := localSeen[cmd]; seen {
			return
		}

		localSeen[cmd] = struct{}{}
		out = append(out, cmd)
	}

	for _, dep := range nodeDeps {
		switch {
		case isCommandKey(dep):
			emit(dep)
		case isNodeKey(dep):
			for _, producer := range snap.Deps[dep] {
				if !isCommandKey(producer) {
					continue
				}

				emit(producer)
			}
		}
	}

	return out
}

// ruleRanThisBuild mirrors reasonForRule's ran-this-build check without
// building a ReasonBucket. Iteration == 0 means the snapshot had no info row
// (test fixtures); in that mode we have no signal to attribute upstream causes
// against and conservatively treat no rule as ran-this-build.
func ruleRanThisBuild(rule RuleResult, iteration int64) bool {
	if iteration == 0 {
		return false
	}

	return rule.Iteration == iteration
}

func isCommandKey(k string) bool { return len(k) > 0 && k[0] == rulePrefixCommand }
func isNodeKey(k string) bool    { return len(k) > 0 && k[0] == rulePrefixNode }
