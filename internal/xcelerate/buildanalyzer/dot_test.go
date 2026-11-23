//go:build unit

package buildanalyzer_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

func TestRenderDot_EmitsDigraphAndNodeFillColors(t *testing.T) {
	report := buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{
			{Key: "CompileA", Tool: "swift", Ran: true, ExitCode: 0, Duration: 200 * time.Millisecond},
			{Key: "CompileB", Tool: "swift", Ran: true, ExitCode: 1, Duration: 100 * time.Millisecond},
			{Key: "PlannedOnly", Tool: "linker", Ran: false, Planned: true},
		},
		Dependencies: []buildanalyzer.Dependency{
			{From: "CompileA", To: "CompileB"},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, buildanalyzer.RenderDot(&buf, report))

	out := buf.String()
	assert.True(t, strings.HasPrefix(out, `digraph "Build Foo" {`))
	assert.True(t, strings.HasSuffix(strings.TrimSpace(out), "}"))

	assert.Contains(t, out, "#B4F8B4", "green fill color present (ran OK task)")
	assert.Contains(t, out, "#F8B4B4", "red fill color present (ran failed task)")
	assert.Contains(t, out, "#D0D0D0", "grey fill color present (planned-not-ran task)")

	// Node label carries the Task key — confirm via substring, independent of
	// quoting style.
	assert.Contains(t, out, "CompileA")
	assert.Contains(t, out, "CompileB")
	assert.Contains(t, out, "PlannedOnly")
	assert.Contains(t, out, "(planned)")

	// Edge is rendered.
	assert.Contains(t, out, "->")
}

func TestRenderDot_DeterministicOrdering(t *testing.T) {
	// Same report rendered twice (different Tasks order on input) produces
	// byte-identical output — the renderer sorts by Key internally.
	tasksA := []buildanalyzer.Task{
		{Key: "A", Ran: true},
		{Key: "B", Ran: true},
		{Key: "C", Ran: true},
	}
	tasksB := []buildanalyzer.Task{
		{Key: "C", Ran: true},
		{Key: "A", Ran: true},
		{Key: "B", Ran: true},
	}

	var a, b bytes.Buffer
	require.NoError(t, buildanalyzer.RenderDot(&a, buildanalyzer.Report{Tasks: tasksA}))
	require.NoError(t, buildanalyzer.RenderDot(&b, buildanalyzer.Report{Tasks: tasksB}))

	assert.Equal(t, a.String(), b.String())
}

func TestRenderDot_EmptyReport(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, buildanalyzer.RenderDot(&buf, buildanalyzer.Report{}))

	out := buf.String()
	assert.Contains(t, out, "digraph")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(out), "}"))
}

func TestRenderDot_EscapesQuotesInLabels(t *testing.T) {
	// A Task whose Key contains a `"` — the renderer uses `%q` for quoting so
	// it should emit `\"` and remain parseable.
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{Key: `Ld "with quotes"`, Ran: true}},
	}

	var buf bytes.Buffer
	require.NoError(t, buildanalyzer.RenderDot(&buf, report))

	out := buf.String()
	assert.Contains(t, out, `\"with quotes\"`)
}
