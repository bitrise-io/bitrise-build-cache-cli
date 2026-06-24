package doctor

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

type State string

const (
	StateOK    State = "ok"
	StateWarn  State = "warn"
	StateError State = "error"
)

type Result struct {
	State   State  `json:"state"`
	Detail  string `json:"detail"`
	Fixable bool   `json:"fixable"`
}

type Check struct {
	Name     string                               `json:"name"`
	Diagnose func(context.Context) Result         `json:"-"`
	Fix      func() (fixDetail string, err error) `json:"-"`
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
	ApplyFixes      bool
	SkipUpdateCheck bool
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
	}
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
	checks := d.checks(opts.SkipUpdateCheck)
	items := make([]ReportItem, 0, len(checks))

	for _, c := range checks {
		res := c.Diagnose(ctx)
		item := ReportItem{Name: c.Name, Result: res}

		if opts.ApplyFixes && res.Fixable && c.Fix != nil {
			detail, fxerr := c.Fix()
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

func (d *Doctor) checks(skipUpdateCheck bool) []Check {
	checks := []Check{
		d.authCheck(),
		d.keychainSmokeCheck(),
		d.xcelerateProxyCheck(),
		d.ccacheHelperCheck(),
		d.ccacheBinaryCheck(),
		d.logDirsCheck(),
		d.xcelerateXcconfigCheck(),
	}

	if !skipUpdateCheck {
		checks = append(checks, d.cliVersionCheck())
	}

	return checks
}
