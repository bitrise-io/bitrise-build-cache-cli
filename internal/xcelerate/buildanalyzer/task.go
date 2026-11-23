// Package buildanalyzer post-mortem reads Xcode build artifacts and reports
// what ran. v0 reads only the gzipped .xcactivitylog and emits a flat list
// of executed command sections. Phases v1+ fold in the llbuild manifest,
// build.db signature diff, and clang .dia diagnostics — see
// docs/xcode-build-analyzer.md.
package buildanalyzer

import "time"

// Task is a single IDEActivityLogCommandInvocationSection, trimmed to the
// fields v0 can reliably lift from the log alone. Fields that depend on the
// manifest (inputs/outputs, tool from llbuild, exec reason) stay out of v0.
type Task struct {
	// Key is the section signature — the stable identifier Xcode and llbuild
	// share, e.g. `Ld /path/Foo normal arm64`.
	Key string `json:"key"`
	// Target is a best-effort extraction of the target name from the section
	// signature; empty when the signature doesn't include a `in target 'X'`
	// clause (preparation / compute-graph sections).
	Target string `json:"target,omitempty"`
	// Tool is a best-effort first-word of the signature (`Ld`, `SwiftCompile`,
	// `CompileC`, …). Not canonical — v1 overrides with the manifest tool.
	Tool          string        `json:"tool,omitempty"`
	StartedAt     time.Time     `json:"startedAt"`
	Duration      time.Duration `json:"duration"`
	ExitCode      int           `json:"exitCode"`
	StdoutExcerpt string        `json:"stdoutExcerpt,omitempty"`
	StderrExcerpt string        `json:"stderrExcerpt,omitempty"`
	Errors        []string      `json:"errors,omitempty"`
}

// BuildMeta captures the top-level log metadata — the enclosing
// IDEActivityLogSection that wraps everything.
type BuildMeta struct {
	Title      string        `json:"title,omitempty"`
	StartedAt  time.Time     `json:"startedAt"`
	FinishedAt time.Time     `json:"finishedAt"`
	Duration   time.Duration `json:"duration"`
}

// Report is the JSON shape rendered by `xcode analyze`. Mirrors the design
// doc's `Report` but trimmed to v0 fields; v1+ extend without a schema break.
type Report struct {
	Build BuildMeta `json:"build"`
	Tasks []Task    `json:"tasks"`
}
