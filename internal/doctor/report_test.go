//go:build unit

package doctor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

func TestFilterChecks_Only(t *testing.T) {
	all := []Check{{Name: "auth"}, {Name: "auth-backend"}, {Name: "ccache-binary"}}

	assert.Len(t, filterChecks(all, nil), 3, "empty Only runs everything")

	got := filterChecks(all, []string{"auth-backend", "auth", "nonexistent"})
	require.Len(t, got, 2)
	assert.Equal(t, "auth", got[0].Name, "check order is preserved, not the Only order")
	assert.Equal(t, "auth-backend", got[1].Name)
}

func TestChecks_XcodeSubsetIsRunnable(t *testing.T) {
	d := NewDoctor()
	d.ActivatedTools = nil

	for _, names := range [][]string{XcodeCheckNames, AuthProbeCheckNames} {
		checks := d.checks(Options{Only: names})

		got := make([]string, 0, len(checks))
		for _, c := range checks {
			got = append(got, c.Name)
			assert.NotNil(t, c.Diagnose, "%s must be diagnosable", c.Name)
		}
		assert.ElementsMatch(t, names, got, "every named check must exist")
	}
}

func TestIssueLines(t *testing.T) {
	fixed := "restarted"
	r := Report{Items: []ReportItem{
		{Name: "auth", Result: Result{State: StateOK, Detail: "fine"}},
		{Name: "xcelerate-proxy", Result: Result{State: StateWarn, Detail: "not running"}},
		{Name: "log-dirs", Result: Result{State: StateError, Detail: "not writable"}},
		{Name: "ccache-helper", Result: Result{State: StateWarn, Detail: "stuck"}, FixResult: &fixed},
	}}

	assert.Equal(t, []string{
		"warn xcelerate-proxy: not running",
		"error log-dirs: not writable",
	}, IssueLines(r), "OK and fixed items are not issues")

	assert.Nil(t, IssueLines(Report{Items: []ReportItem{{Name: "auth", Result: Result{State: StateOK}}}}))

	assert.Equal(t, []string{"ok auth: fine", "warn proxy: down"}, Lines([]ReportItem{
		{Name: "auth", Result: Result{State: StateOK, Detail: "fine"}},
		{Name: "proxy", Result: Result{State: StateWarn, Detail: "down"}},
	}), "Lines keeps the healthy items, for debug logging")
}

func TestEffectiveOverall(t *testing.T) {
	fixed := "done"
	warn := ReportItem{Name: "a", Result: Result{State: StateWarn}}
	errItem := ReportItem{Name: "b", Result: Result{State: StateError}}
	fixedErr := ReportItem{Name: "c", Result: Result{State: StateError}, FixResult: &fixed}

	assert.Equal(t, StateOK, EffectiveOverall(Report{Items: []ReportItem{fixedErr}}))
	assert.Equal(t, StateWarn, EffectiveOverall(Report{Items: []ReportItem{warn, fixedErr}}))
	assert.Equal(t, StateError, EffectiveOverall(Report{Items: []ReportItem{warn, errItem}}))
}

func TestXcodeCheckNames_ExcludesBackendProbe(t *testing.T) {
	assert.NotContains(t, XcodeCheckNames, "auth-backend")
	assert.Contains(t, AuthProbeCheckNames, "auth-backend")
}

func TestStateDirCandidates_ScopedToActivatedTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := paths.FromHome(home)

	activated := func(tools ...toolconfig.Tool) func() map[toolconfig.Tool]bool {
		return func() map[toolconfig.Tool]bool {
			out := map[toolconfig.Tool]bool{}
			for _, tool := range tools {
				out[tool] = true
			}

			return out
		}
	}

	d := &Doctor{ActivatedTools: activated(toolconfig.Xcelerate)}
	assert.Equal(t, []string{p.XcelerateLogDir()}, d.stateDirCandidates(),
		"an Xcode-only setup must not be asked about ccache's log dir")

	d = &Doctor{ActivatedTools: activated(toolconfig.Ccache)}
	assert.Equal(t, []string{p.CcacheLogDir()}, d.stateDirCandidates())

	d = &Doctor{ActivatedTools: activated(toolconfig.Xcelerate, toolconfig.Ccache)}
	assert.Equal(t, []string{p.XcelerateLogDir(), p.CcacheLogDir()}, d.stateDirCandidates())

	d = &Doctor{ActivatedTools: activated(toolconfig.Gradle)}
	assert.Empty(t, d.stateDirCandidates(), "Gradle writes no CLI-managed logs")

	res := (&Doctor{ActivatedTools: activated(toolconfig.Gradle)}).logDirsCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.False(t, res.Fixable)

	explicit := []string{"/tmp/injected"}
	assert.Equal(t, explicit, (&Doctor{StateDirCandidates: explicit}).stateDirCandidates(),
		"an explicit list still wins")
}

// The probe must test the credential the caller pins, so it can't pass on a
// different one the machine happens to resolve (a CI JWT, say).
func TestAuthBackendCheck_PinnedCredentialIsUsed(t *testing.T) {
	var probed common.CacheAuthConfig

	d := NewDoctor()
	d.Envs = map[string]string{common.EnvJWT: "a-ci-jwt-that-would-otherwise-win"}
	d.BackendProbe = func(_ context.Context, cfg common.CacheAuthConfig, _ map[string]string) (time.Duration, error) {
		probed = cfg

		return time.Millisecond, nil
	}

	pinned := common.CacheAuthConfig{AuthToken: "the-token-the-build-used", WorkspaceID: "ws-1"}

	res := d.authBackendCheck(&pinned).Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Equal(t, pinned, probed, "the pinned credential must be the one probed")
}

func TestAuthBackendCheck_EmptyPinnedCredentialIsAnError(t *testing.T) {
	d := NewDoctor()
	d.Envs = map[string]string{common.EnvJWT: "a-ci-jwt-that-would-otherwise-win"}
	d.BackendProbe = func(context.Context, common.CacheAuthConfig, map[string]string) (time.Duration, error) {
		t.Fatal("an empty credential must not reach the backend")

		return 0, nil
	}
	res := d.authBackendCheck(&common.CacheAuthConfig{}).Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
	assert.Contains(t, res.Detail, "cannot authenticate")
}

func TestRun_OnlyRunsSelectedChecks(t *testing.T) {
	d := NewDoctor()
	d.ActivatedTools = nil

	report := d.Run(context.Background(), Options{
		Only:             []string{"log-dirs"},
		SkipUpdateCheck:  true,
		SkipBackendProbe: true,
	})

	require.Len(t, report.Items, 1)
	assert.Equal(t, "log-dirs", report.Items[0].Name)
}

// A pinned credential has no AuthSource, and reporting "source=none" reads as
// "nothing resolvable" — the opposite of what happened.
func TestAuthBackendCheck_PinnedCredentialIsNamedInTheDetail(t *testing.T) {
	pinned := common.CacheAuthConfig{AuthToken: "tok", WorkspaceID: "ws-1"}

	d := NewDoctor()
	d.Envs = map[string]string{}
	d.BackendProbe = func(context.Context, common.CacheAuthConfig, map[string]string) (time.Duration, error) {
		return time.Millisecond, nil
	}

	res := d.authBackendCheck(&pinned).Diagnose(context.Background())

	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "the credential this build used")
	assert.NotContains(t, res.Detail, "source=none")
}
