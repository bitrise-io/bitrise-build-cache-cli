package doctor

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/refresh"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type State string

const (
	StateOK    State = "ok"
	StateWarn  State = "warn"
	StateError State = "error"
)

type Fixer interface {
	Fix() (detail string, err error)
}

type Result struct {
	State   State  `json:"state"`
	Detail  string `json:"detail"`
	Fixable bool   `json:"fixable"`
	Fixer   Fixer  `json:"-"`
}

type Check struct {
	Name     string                       `json:"name"`
	Diagnose func(context.Context) Result `json:"-"`
}

type Report struct {
	Items   []ReportItem `json:"items"`
	Version string       `json:"cli_version"`
	Overall State        `json:"overall"`
}

type ReportItem struct {
	Name      string  `json:"name"`
	Result    Result  `json:"result"`
	FixResult *string `json:"fix_result,omitempty"`
	FixError  string  `json:"fix_error,omitempty"`
}

func computeOverall(items []ReportItem) State {
	worst := StateOK
	for _, it := range items {
		switch it.Result.State {
		case StateError:
			return StateError
		case StateWarn:
			worst = StateWarn
		case StateOK:
		}
	}

	return worst
}

type Options struct {
	ApplyFixes       bool
	SkipUpdateCheck  bool
	SkipBackendProbe bool
	// Only, when non-empty, restricts the run to checks with these names
	// (see XcodeCheckNames, AuthProbeCheckNames).
	Only []string
}

type Doctor struct {
	OsProxy    utils.OsProxy
	Envs       map[string]string
	CLIVersion string
	HTTPClient *http.Client
	// AuthBackends overrides the credential stores the checks read; nil means the
	// real keychain and config file.
	AuthBackends []store.Store
	Keyring      keychain.Backend
	LookPath     func(string) (string, error)
	// StateDirCandidates are the log dirs to check; nil derives them from the
	// activated tools, so an Xcode-only setup isn't asked about ccache's.
	StateDirCandidates []string
	LatestReleaseTag   func(ctx context.Context, c *http.Client) (string, error)
	ActivatedTools     func() map[toolconfig.Tool]bool
	BackendProbe       BackendProbeFunc
	// AuthFixPrompt collects credentials for the auth fixer. Nil falls back to
	// the token prompt; the doctor command supplies one that offers a browser
	// sign-in first.
	AuthFixPrompt func() (workspaceID, authToken string, err error)
	Now           func() time.Time
	Debug         bool

	// checksOverride replaces the real check set in tests.
	checksOverride []Check
}

func NewDoctor() *Doctor {
	osProxy := utils.DefaultOsProxy{}

	return &Doctor{
		OsProxy:          osProxy,
		Envs:             utils.AllEnvs(),
		CLIVersion:       common.GetCLIVersion(nil),
		HTTPClient:       &http.Client{Timeout: 3 * time.Second},
		Keyring:          keychain.NewBackend(),
		LookPath:         exec.LookPath,
		LatestReleaseTag: fetchLatestGitHubRelease,
		ActivatedTools:   defaultActivatedTools,
	}
}

func (d *Doctor) toolActivated(t toolconfig.Tool) bool {
	if d.ActivatedTools == nil {
		return true
	}

	return d.ActivatedTools()[t]
}

func defaultActivatedTools() map[toolconfig.Tool]bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	out := map[toolconfig.Tool]bool{}
	for _, s := range refresh.Scan(home) {
		out[s.Tool] = true
	}

	return out
}

// stateDirCandidates resolves the log dirs to check, limited to the tools that
// are actually activated.
func (d *Doctor) stateDirCandidates() []string {
	if d.StateDirCandidates != nil {
		return d.StateDirCandidates
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	p := paths.FromHome(home)

	var out []string
	if d.toolActivated(toolconfig.Xcelerate) {
		out = append(out, p.XcelerateLogDir())
	}
	if d.toolActivated(toolconfig.Ccache) {
		out = append(out, p.CcacheLogDir())
	}

	return out
}

func (d *Doctor) Run(ctx context.Context, opts Options) Report {
	report := d.Diagnose(ctx, opts)
	if !opts.ApplyFixes {
		return report
	}

	for i := range report.Items {
		ApplyFix(&report.Items[i])
	}
	report.Overall = computeOverall(report.Items)

	return report
}

// Diagnose runs the checks without fixing anything, so a caller can show the
// results and decide what to repair before touching the machine.
func (d *Doctor) Diagnose(ctx context.Context, opts Options) Report {
	checks := d.checks(opts)
	items := make([]ReportItem, 0, len(checks))

	for _, c := range checks {
		items = append(items, ReportItem{Name: c.Name, Result: c.Diagnose(ctx)})
	}

	return Report{Items: items, Version: d.CLIVersion, Overall: computeOverall(items)}
}

// NeedsTerminal reports whether a fixer has to prompt, so a scripted run can
// skip it instead of letting it fail on a missing TTY.
func NeedsTerminal(f Fixer) bool {
	t, ok := f.(interface{ NeedsTerminal() bool })

	return ok && t.NeedsTerminal()
}

// ApplyFixesUnattended repairs everything fixable without prompting, noting the
// fixers it had to skip for needing a terminal.
func ApplyFixesUnattended(report *Report) {
	for i := range report.Items {
		f := report.Items[i].Result.Fixer
		if f == nil {
			continue
		}
		if NeedsTerminal(f) {
			report.Items[i].FixError = "needs a prompt — rerun with `--fix --interactive`"

			continue
		}

		ApplyFix(&report.Items[i])
	}
	report.Overall = computeOverall(report.Items)
}

// ApplyFix runs an item's fixer and records the outcome on the item. No-op for
// items without one.
func ApplyFix(item *ReportItem) {
	if item.Result.Fixer == nil {
		return
	}

	detail, err := item.Result.Fixer.Fix()
	if err != nil {
		item.FixError = err.Error()

		return
	}

	item.FixResult = &detail
}

// Fixable lists the items that have a fixer, in check order.
func Fixable(items []ReportItem) []ReportItem {
	out := make([]ReportItem, 0, len(items))
	for _, it := range items {
		if it.Result.Fixer != nil {
			out = append(out, it)
		}
	}

	return out
}

func (d *Doctor) checks(opts Options) []Check {
	if d.checksOverride != nil {
		return filterChecks(d.checksOverride, opts.Only)
	}

	checks := []Check{
		d.authCheck(),
		d.keychainSmokeCheck(),
	}

	if !opts.SkipBackendProbe {
		checks = append(checks, d.authBackendCheck())
	}

	checks = append(checks,
		d.xcelerateProxyCheck(),
		d.xcelerateWrapperPathCheck(),
		d.enrichmentCheck(),
		d.ccacheHelperCheck(),
		d.ccacheBinaryCheck(),
		d.logDirsCheck(),
	)

	if !opts.SkipUpdateCheck {
		checks = append(checks, d.cliVersionCheck())
	}

	return filterChecks(checks, opts.Only)
}

func filterChecks(checks []Check, only []string) []Check {
	if len(only) == 0 {
		return checks
	}

	wanted := make(map[string]bool, len(only))
	for _, n := range only {
		wanted[n] = true
	}

	out := make([]Check, 0, len(only))
	for _, c := range checks {
		if wanted[c.Name] {
			out = append(out, c)
		}
	}

	return out
}

func (d *Doctor) osProxy() utils.OsProxy {
	if d.OsProxy != nil {
		return d.OsProxy
	}

	return utils.DefaultOsProxy{}
}
