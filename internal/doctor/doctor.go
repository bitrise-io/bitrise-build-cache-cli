package doctor

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
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
}

type ReportItem struct {
	Name      string  `json:"name"`
	Result    Result  `json:"result"`
	FixResult *string `json:"fix_result,omitempty"`
	FixError  string  `json:"fix_error,omitempty"`
}

func (r Report) Overall() State {
	worst := StateOK
	for _, it := range r.Items {
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
}

type Doctor struct {
	OsProxy            utils.OsProxy
	Envs               map[string]string
	CLIVersion         string
	HTTPClient         *http.Client
	AuthLoader         common.AuthLoader
	Keyring            keychain.Backend
	LookPath           func(string) (string, error)
	StateDirCandidates []string
	LatestReleaseTag   func(ctx context.Context, c *http.Client) (string, error)
	ActivatedTools     func() map[toolconfig.Tool]bool
	BackendProbe       BackendProbeFunc
	Now                func() time.Time
	Debug              bool
}

func NewDoctor() *Doctor {
	osProxy := utils.DefaultOsProxy{}

	return &Doctor{
		OsProxy:            osProxy,
		Envs:               utils.AllEnvs(),
		CLIVersion:         common.GetCLIVersion(nil),
		HTTPClient:         &http.Client{Timeout: 3 * time.Second},
		AuthLoader:         keychain.New(),
		Keyring:            keychain.NewBackend(),
		LookPath:           exec.LookPath,
		StateDirCandidates: defaultStateDirCandidates(),
		LatestReleaseTag:   fetchLatestGitHubRelease,
		ActivatedTools:     defaultActivatedTools,
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

func defaultStateDirCandidates() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	p := paths.FromHome(home)

	return []string{p.XcelerateLogDir(), p.CcacheLogDir()}
}

func (d *Doctor) Run(ctx context.Context, opts Options) Report {
	checks := d.checks(opts)
	items := make([]ReportItem, 0, len(checks))

	for _, c := range checks {
		res := c.Diagnose(ctx)
		item := ReportItem{Name: c.Name, Result: res}

		if opts.ApplyFixes && res.Fixer != nil {
			detail, fxerr := res.Fixer.Fix()
			if fxerr != nil {
				item.FixError = fxerr.Error()
			} else {
				item.FixResult = &detail
			}
		}

		items = append(items, item)
	}

	return Report{Items: items, Version: d.CLIVersion}
}

func (d *Doctor) checks(opts Options) []Check {
	checks := []Check{
		d.authCheck(),
		d.keychainSmokeCheck(),
	}

	if !opts.SkipBackendProbe {
		checks = append(checks, d.authBackendCheck())
	}

	checks = append(checks,
		d.xcelerateProxyCheck(),
		d.enrichmentCheck(),
		d.ccacheHelperCheck(),
		d.ccacheBinaryCheck(),
		d.logDirsCheck(),
	)

	if !opts.SkipUpdateCheck {
		checks = append(checks, d.cliVersionCheck())
	}

	return checks
}
