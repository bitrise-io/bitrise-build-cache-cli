package buildanalyzer

// Severity is the compiler-diagnostic severity surfaced on Task.Diagnostics.
// Values map 1:1 to clang / swiftc's "note", "warning", "error", "fatal"
// labels (clang also emits "remark", folded under "note" for v3).
type Severity string

const (
	SeverityNote    Severity = "note"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
	SeverityFatal   Severity = "fatal"
)

// Range is a source location span attached to a Diagnostic — a {file,
// startLine, startCol, endLine, endCol} tuple. Populated by the `.dia` reader
// when it ships; the stderr fallback leaves ranges empty.
type Range struct {
	File      string `json:"file,omitempty"`
	StartLine int    `json:"startLine,omitempty"`
	StartCol  int    `json:"startCol,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndCol    int    `json:"endCol,omitempty"`
}

// FixIt is a clang-emitted suggested edit paired to a Diagnostic — the Range
// to replace and the replacement text. Only surfaced by the `.dia` reader.
type FixIt struct {
	Range       Range  `json:"range"`
	Replacement string `json:"replacement,omitempty"`
}

// Diagnostic is one compiler-emitted message attached to a Task. v3 populates
// it from two sources with equal shape so downstream consumers (pretty
// renderer, future BE push) don't branch on origin:
//
//   - stderr fallback: lifts `path:line:col: severity: message` from the
//     Task's StderrExcerpt blob. Always available; sole source for Swift
//     (swiftc doesn't emit `.dia`). Ranges + FixIts stay empty.
//   - `.dia` reader (deferred — see docs/xcode-build-analyzer.md): richer
//     clang serialized diagnostics with ranges, fix-its, notes, categories.
//
// The Source field records which reader produced the record so a BE roll-up
// can tell "we saw this via stderr only" from "clang gave us the full .dia".
type Diagnostic struct {
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Column   int      `json:"column,omitempty"`
	Severity Severity `json:"severity,omitempty"`
	Message  string   `json:"message,omitempty"`
	Category string   `json:"category,omitempty"`
	Ranges   []Range  `json:"ranges,omitempty"`
	FixIts   []FixIt  `json:"fixIts,omitempty"`
	Source   string   `json:"source,omitempty"`
}
