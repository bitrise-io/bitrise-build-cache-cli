//go:build unit

package doctor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/doctor"
)

func okItem(name string) doctorpkg.ReportItem {
	return doctorpkg.ReportItem{
		Name:   name,
		Result: doctorpkg.Result{State: doctorpkg.StateOK, Detail: "fine"},
	}
}

func warnItem(name string, fixable bool) doctorpkg.ReportItem {
	return doctorpkg.ReportItem{
		Name:   name,
		Result: doctorpkg.Result{State: doctorpkg.StateWarn, Detail: "needs attention", Fixable: fixable},
	}
}

func errorItem(name string) doctorpkg.ReportItem {
	return doctorpkg.ReportItem{
		Name:   name,
		Result: doctorpkg.Result{State: doctorpkg.StateError, Detail: "broken"},
	}
}

func render(t *testing.T, items []doctorpkg.ReportItem, fixed bool) string {
	t.Helper()

	r := doctorpkg.Report{Items: items, Version: "v2.8.6"}
	var buf bytes.Buffer
	writeHuman(&buf, r, fixed, effectiveOverall(r), false)

	return buf.String()
}

func TestWriteHuman_groupsIssuesBeforeHealthy(t *testing.T) {
	out := render(t, []doctorpkg.ReportItem{
		okItem("keychain"),
		warnItem("xcelerate-proxy", true),
		okItem("ccache-helper"),
		errorItem("auth"),
	}, false)

	issuesAt := strings.Index(out, "Issues:")
	healthyAt := strings.Index(out, "Healthy:")
	require.NotEqual(t, -1, issuesAt)
	require.NotEqual(t, -1, healthyAt)
	assert.Less(t, issuesAt, healthyAt, "Issues section must precede Healthy")

	issuesSec := out[issuesAt:healthyAt]
	assert.Contains(t, issuesSec, "xcelerate-proxy")
	assert.Contains(t, issuesSec, "auth")
	assert.NotContains(t, issuesSec, "keychain")
	assert.NotContains(t, issuesSec, "ccache-helper")

	healthySec := out[healthyAt:]
	assert.Contains(t, healthySec, "keychain")
	assert.Contains(t, healthySec, "ccache-helper")
	assert.NotContains(t, healthySec, "xcelerate-proxy")
	assert.NotContains(t, healthySec, "auth")
}

func TestWriteHuman_allHealthy_noIssuesHeader(t *testing.T) {
	out := render(t, []doctorpkg.ReportItem{okItem("a"), okItem("b")}, false)

	assert.NotContains(t, out, "Issues:")
	assert.Contains(t, out, "Healthy:")
	assert.Contains(t, out, "Overall: ok")
}

func TestWriteHuman_allIssues_noHealthyHeader(t *testing.T) {
	out := render(t, []doctorpkg.ReportItem{errorItem("a"), warnItem("b", false)}, false)

	assert.Contains(t, out, "Issues:")
	assert.NotContains(t, out, "Healthy:")
	assert.Contains(t, out, "Overall: error")
}

func TestWriteHuman_fixedItemMovesToHealthy(t *testing.T) {
	fixed := "restored socket"
	items := []doctorpkg.ReportItem{
		{
			Name:      "xcelerate-proxy",
			Result:    doctorpkg.Result{State: doctorpkg.StateError, Detail: "socket gone"},
			FixResult: &fixed,
		},
	}

	out := render(t, items, true)
	assert.NotContains(t, out, "Issues:")
	assert.Contains(t, out, "Healthy:")
	assert.Contains(t, out, "fixed: restored socket")
	assert.Contains(t, out, "Overall: ok")
}

func TestWriteHuman_fixErrorStaysInIssues(t *testing.T) {
	items := []doctorpkg.ReportItem{
		{
			Name:     "auth",
			Result:   doctorpkg.Result{State: doctorpkg.StateError, Detail: "no token", Fixable: true},
			FixError: "prompt cancelled",
		},
	}

	out := render(t, items, true)
	issuesAt := strings.Index(out, "Issues:")
	require.NotEqual(t, -1, issuesAt)
	assert.Contains(t, out[issuesAt:], "fix failed:")
	assert.Contains(t, out[issuesAt:], "prompt cancelled")
	assert.NotContains(t, out, "Healthy:")
}

func TestWriteHuman_fixableHintOnlyWithoutFix(t *testing.T) {
	items := []doctorpkg.ReportItem{warnItem("xcelerate-proxy", true)}

	withoutFix := render(t, items, false)
	assert.Contains(t, withoutFix, "rerun with --fix to repair")

	withFix := render(t, items, true)
	assert.NotContains(t, withFix, "rerun with --fix to repair")
}
