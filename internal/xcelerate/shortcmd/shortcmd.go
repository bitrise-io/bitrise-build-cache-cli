// Package shortcmd renders the "shortCommand" string used on the BE
// invocation row: "<action> [<scheme> / <testPlan> / <configuration>]".
// Shared by the wrapper (argv-driven) and the wrapperless (manifest-driven)
// flows so both land identically formatted rows.
package shortcmd

import "strings"

// Render returns "<action> [<scheme> / <testPlan> / <configuration>]" with
// missing fields skipped. Empty action returns just the bracket. Empty
// scheme/testPlan/configuration returns just the action. All empty returns "".
func Render(action, scheme, testPlan, configuration string) string {
	suffix := Suffix(scheme, testPlan, configuration)

	switch {
	case action == "" && suffix == "":
		return ""
	case action == "":
		return suffix
	case suffix == "":
		return action
	}

	return action + " " + suffix
}

// Suffix returns the "[scheme / testPlan / configuration]" bracket alone, or
// "" when all three are empty.
func Suffix(scheme, testPlan, configuration string) string {
	parts := make([]string, 0, 3)
	if scheme != "" {
		parts = append(parts, scheme)
	}
	if testPlan != "" {
		parts = append(parts, testPlan)
	}
	if configuration != "" {
		parts = append(parts, configuration)
	}
	if len(parts) == 0 {
		return ""
	}

	return "[" + strings.Join(parts, " / ") + "]"
}
