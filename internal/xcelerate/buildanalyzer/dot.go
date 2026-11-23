package buildanalyzer

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// RenderDot writes a Graphviz `digraph` representation of report to out.
// Nodes: green=ran OK, red=ran failed, grey=planned-not-ran.
//
// Pure string building, zero third-party deps — `dot(1)` accepts any stream
// with the right grammar, and the subset used here (quoted labels, fillcolor,
// subgraph-less edges) is universal across Graphviz versions.
func RenderDot(out io.Writer, report Report) error {
	title := "Xcode build"
	if report.Build.Title != "" {
		title = report.Build.Title
	}

	var b strings.Builder
	fmt.Fprintf(&b, "digraph %q {\n", title)
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=\"rounded,filled\", fontname=\"Menlo\", fontsize=10];\n")
	b.WriteString("  edge [color=\"#888888\"];\n\n")

	// Deterministic node emission: sort by Key so repeated runs on the same
	// report produce byte-identical output (easier CI diffing).
	tasks := make([]Task, len(report.Tasks))
	copy(tasks, report.Tasks)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Key < tasks[j].Key })

	for _, task := range tasks {
		id := dotID(task.Key)
		label := dotLabel(task)
		fill := dotFill(task)
		fmt.Fprintf(&b, "  %s [label=%q, fillcolor=%q];\n", id, label, fill)
	}

	if len(report.Dependencies) > 0 {
		b.WriteString("\n")

		deps := make([]Dependency, len(report.Dependencies))
		copy(deps, report.Dependencies)
		sort.Slice(deps, func(i, j int) bool {
			if deps[i].From != deps[j].From {
				return deps[i].From < deps[j].From
			}

			return deps[i].To < deps[j].To
		})

		for _, dep := range deps {
			fmt.Fprintf(&b, "  %s -> %s;\n", dotID(dep.From), dotID(dep.To))
		}
	}

	b.WriteString("}\n")

	if _, err := io.WriteString(out, b.String()); err != nil {
		return fmt.Errorf("write dot: %w", err)
	}

	return nil
}

// dotID derives a stable Graphviz identifier from a Task Key. Quoted IDs are
// legal in dot, but single-word identifiers are easier to read when a graph
// is opened in a text editor — so we fold the first ~60 chars + a hash digit.
// The ID isn't user-visible (the label carries the full text), so the only
// requirement is uniqueness.
func dotID(key string) string {
	h := fnvHash(key)

	return fmt.Sprintf("n%016x", h)
}

// dotLabel renders a two-line label: the key (truncated) with an optional
// priority suffix, then a small metadata line with tool + duration + exit
// when the task ran.
func dotLabel(task Task) string {
	primary := truncateLabel(task.Key, 80)
	if task.Priority > 0 {
		primary = fmt.Sprintf("%s [P%d]", primary, task.Priority)
	}
	if !task.Ran {
		return primary + "\\n(planned)"
	}

	suffix := task.Tool
	if task.Duration > 0 {
		suffix = fmt.Sprintf("%s  %s", suffix, task.Duration.Round(1_000_000))
	}
	if task.ExitCode != 0 {
		suffix = fmt.Sprintf("%s  exit=%d", suffix, task.ExitCode)
	}

	return primary + "\\n" + suffix
}

// dotFill picks the node color — follows the design-doc palette.
func dotFill(task Task) string {
	switch {
	case !task.Ran:
		return "#D0D0D0" // grey — planned but not executed
	case task.ExitCode != 0:
		return "#F8B4B4" // red — ran and failed
	default:
		return "#B4F8B4" // green — ran OK
	}
}

func truncateLabel(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	return s[:limit-1] + "…"
}

// fnvHash is an inline FNV-1a 64-bit; stdlib hash/fnv would do, but the
// renderer already imports strings/io and inlining keeps the import block
// small. Only uniqueness matters, no cryptographic property.
func fnvHash(s string) uint64 {
	const (
		offset uint64 = 1469598103934665603
		prime  uint64 = 1099511628211
	)

	h := offset
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= prime
	}

	return h
}
