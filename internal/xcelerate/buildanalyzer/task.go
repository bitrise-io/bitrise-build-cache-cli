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

	// ExecutionReason is populated by Classify (v2) from the build.db join.
	// Empty when no build.db was read; see ExecutionReason constants for the
	// four buckets.
	ExecutionReason ExecutionReason `json:"executionReason,omitempty"`
	// Iteration is the llbuild build-counter the rule row carries — equal to
	// the snapshot's current iteration when the task ran this build, lower
	// when its result was reused from an earlier build. Zero when no row was
	// matched.
	Iteration int64 `json:"iteration,omitempty"`
	// Signature is the llbuild rule signature hex-encoded. This is the
	// key-identity hash, stable per rule key — it does NOT change when the
	// rule's output/inputs change (that signal lives in the rule's Value
	// BLOB, surfaced as CurrentValueHex when a prior snapshot is available).
	// Empty when the task had no build.db row (cold rule, or no snapshot
	// joined).
	Signature string `json:"signature,omitempty"`

	// Diagnostics are the structured compiler messages v3 attaches via
	// AttachDiagnostics — stderr-extracted today, `.dia`-sourced once that
	// reader ships. One record per `file:line:col: severity: message`
	// header. Empty when the task emitted no recognizable diagnostics.
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`

	// UpstreamRanCount is the number of transitively-upstream Commands that
	// also ran this build. Populated by Classify when a Snapshot with Deps is
	// joined; zero on cold / skipped / unclassified tasks or when no DB was
	// read. On a ran-this-build task, zero means no re-ran ancestor caused
	// this one — i.e. the task is a change entry point (RootCause).
	UpstreamRanCount int `json:"upstreamRanCount,omitempty"`
	// RootCause is true when ExecutionReason == ReasonRanThisBuild and
	// UpstreamRanCount == 0. The "entry points" of each re-ran subgraph —
	// the small set that explains why the build was not a no-op.
	RootCause bool `json:"rootCause,omitempty"`
	// ChangeCauseKeys are upstream Command keys that also ran this build.
	// Direct `C -> N -> C` one-hop walk by default; when the direct scan
	// yields nothing but transitively-upstream Commands DID run, falls
	// through to the nearest-ran-ancestor frontier so the field stays
	// consistent with UpstreamRanCount. Head-of-chain rules (ClangStatCache
	// & co.) are filtered; Gate aggregation rules are walked through
	// transparently. Truncated at 10 entries with a trailing `…` sentinel.
	ChangeCauseKeys []string `json:"changeCauseKeys,omitempty"`

	// SignatureChanged is true when a prior snapshot held this rule with a
	// different serialized rule Value than the current one. Only populated
	// when Snapshot.PriorRules is present — B.2's signature-diff signal.
	// Lets the operator distinguish "ran because an input flipped" from
	// "ran because something upstream re-ran".
	SignatureChanged bool `json:"signatureChanged,omitempty"`
	// PriorSignature is the matched rule's llbuild signature column from the
	// prior snapshot, hex-encoded. This is the key-identity hash, stable per
	// rule key — it does NOT change when the rule's output/inputs change.
	// Kept for backwards compatibility; use PriorValueHex / CurrentValueHex
	// for the actual diff display. Empty when no prior snapshot was
	// available, or when the task is new (no prior rule).
	PriorSignature string `json:"priorSignature,omitempty"`
	// PriorValueHex is the first 16 hex chars of sha256(priorRule.Value) —
	// a short fingerprint of the serialized rule Value BLOB from the prior
	// snapshot. Populated alongside SignatureChanged so the UI can render
	// `prior=<hex> now=<hex>` without needing to pull the raw BLOB. Empty
	// when no diff was detected or no prior rule existed.
	PriorValueHex string `json:"priorValueHex,omitempty"`
	// CurrentValueHex is the first 16 hex chars of sha256(currentRule.Value)
	// — the counterpart to PriorValueHex. Populated whenever
	// SignatureChanged is set.
	CurrentValueHex string `json:"currentValueHex,omitempty"`

	// PriorValueBytesHex is the raw hex of up to the first 128 bytes of
	// priorRule.Value, with a trailing `…` sentinel when truncated. Lets
	// the operator eyeball the BLOB without needing the DB. Populated
	// alongside SignatureChanged.
	PriorValueBytesHex string `json:"priorValueBytesHex,omitempty"`
	// CurrentValueBytesHex is the counterpart for currentRule.Value.
	CurrentValueBytesHex string `json:"currentValueBytesHex,omitempty"`
	// ValueDiffOffset is the byte offset of the first differing byte
	// between prior and current Value. -1 when the common prefix matches
	// entirely but the sizes differ (one side is a prefix of the other).
	// Omitted when both BLOBs are empty.
	ValueDiffOffset int `json:"valueDiffOffset,omitempty"`
	// ValueDiffNote is a one-line human description of the byte diff —
	// e.g. `byte 32: 0x6B52 → 0x8152 (size: 171 → 171 bytes)` or
	// `sizes differ (…); common prefix matches entirely`. Empty when the
	// values are identical byte-for-byte (shouldn't happen when
	// SignatureChanged is set).
	ValueDiffNote string `json:"valueDiffNote,omitempty"`
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

// FileChange names a source file (llbuild Node rule) whose stored Value BLOB
// changed between the prior and current snapshots, together with the
// downstream Command that consumes it. Populated by Classify when a prior
// snapshot is available. Lets the operator read "file X.swift changed →
// triggered compile Y" rather than "ClangStatCache triggered everything",
// which is what the Command-level sig-diff alone collapses to.
type FileChange struct {
	NodeKey           string `json:"nodeKey"`
	DownstreamTaskKey string `json:"downstreamTaskKey"`
	// PriorFile and CurrentFile hold the semantic llbuild FileInfo decode
	// (mode / size / opaque mtime slots) when both Node Values parse as
	// FileInfo. nil on either side when DecodeFileInfo bailed — the
	// byte-level fields below still describe the raw diff in that case.
	PriorFile   *FileInfo `json:"priorFile,omitempty"`
	CurrentFile *FileInfo `json:"currentFile,omitempty"`
	// PriorValueBytesHex / CurrentValueBytesHex / ValueDiffOffset /
	// ValueDiffNote mirror the byte-level diff fields on Task. Populated
	// whether or not the FileInfo decode succeeded — the byte-level view
	// works for any rule kind.
	PriorValueBytesHex   string `json:"priorValueBytesHex,omitempty"`
	CurrentValueBytesHex string `json:"currentValueBytesHex,omitempty"`
	ValueDiffOffset      int    `json:"valueDiffOffset,omitempty"`
	ValueDiffNote        string `json:"valueDiffNote,omitempty"`
	// SemanticDiff is the FileInfo.Diff rendering — populated only when
	// both PriorFile and CurrentFile decoded. Empty string otherwise.
	SemanticDiff string `json:"semanticDiff,omitempty"`
}

// Report is the JSON shape rendered by `xcode analyze`. v0 fills Build +
// Tasks from the xcactivitylog; v1's JoinManifest extends Tasks in place with
// Planned/Ran/Inputs/Outputs, appends planned-only Tasks (Ran=false), and
// populates Dependencies.
type Report struct {
	Build        BuildMeta    `json:"build"`
	Tasks        []Task       `json:"tasks"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	// RootCauseKeys is the flat sorted list of every Task.Key where
	// RootCause is set — populated by Classify when Snapshot.Deps is
	// available. Lets a reader eyeball "what actually changed" without
	// filtering the Tasks array. Order is lexicographic for deterministic
	// JSON diffs across runs. Head-of-chain rules (ClangStatCache & co.)
	// are filtered out — they fire on every build and would otherwise
	// drown real root causes.
	RootCauseKeys []string `json:"rootCauseKeys,omitempty"`
	// FileChanges is the deduped list of (changed input file, downstream
	// Command) pairs populated by Classify when a prior snapshot is
	// available. Surfaces the real "what source file flipped" trigger that
	// Command-level sig-diffs alone would miss.
	FileChanges []FileChange `json:"fileChanges,omitempty"`
}
