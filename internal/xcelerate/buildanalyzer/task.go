// Package buildanalyzer post-mortem reads Xcode build artifacts and reports
// what ran. v0 reads only the gzipped .xcactivitylog and emits a flat list
// of executed command sections. Phases v1+ fold in the llbuild manifest,
// build.db signature diff, and clang .dia diagnostics — see
// docs/xcode-build-analyzer.md.
package buildanalyzer

import "time"

// Task is a single IDEActivityLogCommandInvocationSection, trimmed to the
// fields v0 can reliably lift from the log alone. v1 extends it with
// manifest-sourced Planned/Ran/Inputs/Outputs when a manifest join succeeds.
type Task struct {
	// Key is the section signature — the stable identifier Xcode and llbuild
	// share, e.g. `Ld /path/Foo normal arm64`.
	Key string `json:"key"`
	// Target is a best-effort extraction of the target name from the section
	// signature; empty when the signature doesn't include a `in target 'X'`
	// clause (preparation / compute-graph sections).
	Target string `json:"target,omitempty"`
	// Tool is a best-effort first-word of the signature (`Ld`, `SwiftCompile`,
	// `CompileC`, …). v1's JoinManifest overrides this with the manifest's
	// `tool` field when a Command match is found — that value is llbuild's
	// internal tool label (`swift-driver-compilation`, `linker`, `shell`, …)
	// and is more canonical than the signature prefix heuristic.
	Tool          string        `json:"tool,omitempty"`
	StartedAt     time.Time     `json:"startedAt"`
	Duration      time.Duration `json:"duration"`
	ExitCode      int           `json:"exitCode"`
	StdoutExcerpt string        `json:"stdoutExcerpt,omitempty"`
	StderrExcerpt string        `json:"stderrExcerpt,omitempty"`
	Errors        []string      `json:"errors,omitempty"`

	// Planned is true when JoinManifest found a Command matching this Task's
	// Key (via signature or description).
	Planned bool `json:"planned,omitempty"`
	// Ran is true when the Task appears in the activity log — i.e. every Task
	// in Report.Tasks starts with Ran=true. The field exists so the renderer
	// sees Planned-only Commands (synthesized Tasks with Ran=false) through
	// the same code path.
	Ran bool `json:"ran,omitempty"`
	// Inputs / Outputs are llbuild-declared file paths from the matched
	// Command. Empty when no manifest match is available.
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
	// Args is the matched llbuild Command's argv (first entry is the tool
	// binary; rest are its flags). Only a small subset of manifest Commands
	// carry args — orchestration / phony / Gate entries omit the field, and
	// the SwiftDriver compilation entry delegates its argv to the driver —
	// so most joined Tasks come out with Args empty.
	Args []string `json:"args,omitempty"`

	// Priority is the llbuild scheduler priority parsed off the manifest key's
	// `P<N>:` prefix (0 when the prefix is absent — phony / Gate / virtual
	// nodes). Lower runs earlier.
	Priority int `json:"priority,omitempty"`
	// ManifestKey preserves the full unparsed llbuild key (with the `P<N>:`
	// prefix) so callers who need to correlate back to a specific manifest
	// entry still can; the primary Key field holds the stripped form that
	// Dependency edges reference.
	ManifestKey string `json:"manifestKey,omitempty"`
}

// BuildMeta captures the top-level log metadata — the enclosing
// IDEActivityLogSection that wraps everything.
type BuildMeta struct {
	Title      string        `json:"title,omitempty"`
	StartedAt  time.Time     `json:"startedAt"`
	FinishedAt time.Time     `json:"finishedAt"`
	Duration   time.Duration `json:"duration"`
}

// Dependency is a directed edge in the task graph, derived by v1 from the
// manifest commands' input/output file paths: when Command A lists file F as
// an output and Command B lists F as an input, B depends on A and the join
// emits `Dependency{From: A.Key, To: B.Key}`.
type Dependency struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Report is the JSON shape rendered by `xcode analyze`. v0 fills Build +
// Tasks from the xcactivitylog; v1's JoinManifest extends Tasks in place with
// Planned/Ran/Inputs/Outputs, appends planned-only Tasks (Ran=false), and
// populates Dependencies.
type Report struct {
	Build        BuildMeta    `json:"build"`
	Tasks        []Task       `json:"tasks"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}
