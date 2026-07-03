// Package mirrors is the public API for the `activate gradle-mirrors` command:
// it installs a Gradle init script that redirects repository requests to
// Bitrise-hosted mirrors for faster dependency resolution.
package mirrors

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	mirrorsconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle/mirrors"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/envexport"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ActivatorParams configures the Gradle mirrors activation.
type ActivatorParams struct {
	// GradleHome is expanded as-is; empty resolves GRADLE_USER_HOME, else ~/.gradle.
	GradleHome string

	// ProjectRoot is the directory scanned for scope-gap warnings (Gradle
	// scripts using `apply(from = ...)`). Empty falls back to the current
	// working directory; "-" disables scanning.
	ProjectRoot string

	// SelectedFlags lists the mirror flag names to enable (e.g. "mavencentral",
	// "google"). Empty means all mirrors in mirrorsconfig.KnownMirrors.
	SelectedFlags []string

	// Datacenter overrides the BITRISE_DEN_VM_DATACENTER env var. Empty falls
	// back to the env var.
	Datacenter string

	// Enabled overrides the BITRISE_MAVENCENTRAL_PROXY_ENABLED env var. Nil
	// falls back to the env var (true iff the value equals "true").
	Enabled *bool

	// Envs is the env var source consulted when Datacenter or Enabled need
	// fallback. Nil means utils.AllEnvs().
	Envs map[string]string

	// DebugLogging toggles debug logging on the default logger. Ignored when
	// Logger is set.
	DebugLogging bool

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger

	// OsProxy overrides the default OS proxy. If nil, utils.DefaultOsProxy{} is used.
	OsProxy utils.OsProxy

	// PathModifier overrides the default path modifier used for tilde
	// expansion. If nil, pathutil.NewPathModifier() is used.
	PathModifier pathutil.PathModifier
}

// Activator activates Bitrise repository mirrors for Gradle.
type Activator struct {
	logger       log.Logger
	osProxy      utils.OsProxy
	pathModifier pathutil.PathModifier

	gradleHome    string
	projectRoot   string
	selectedFlags []string
	datacenter    string
	enabled       *bool
	envs          map[string]string
}

// NewActivator creates an Activator with production defaults.
func NewActivator(params ActivatorParams) *Activator {
	logger := params.Logger
	if logger == nil {
		logger = log.NewLogger(log.WithDebugLog(params.DebugLogging))
	}

	osProxy := params.OsProxy
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	pathModifier := params.PathModifier
	if pathModifier == nil {
		pathModifier = pathutil.NewPathModifier()
	}

	envs := params.Envs
	if envs == nil {
		envs = utils.AllEnvs()
	}

	return &Activator{
		logger:       logger,
		osProxy:      osProxy,
		pathModifier: pathModifier,

		gradleHome:    params.GradleHome,
		projectRoot:   params.ProjectRoot,
		selectedFlags: params.SelectedFlags,
		datacenter:    params.Datacenter,
		enabled:       params.Enabled,
		envs:          envs,
	}
}

// Activate installs the Gradle mirrors init script when activation is enabled.
// When disabled (via the Enabled param or the BITRISE_MAVENCENTRAL_PROXY_ENABLED
// env var), or when no datacenter is available, Activate logs the reason and
// returns nil.
//
// The installed init script also re-reads BITRISE_MAVENCENTRAL_PROXY_ENABLED at
// Gradle build time: an explicit "false" disables the mirrors at runtime, so a
// customer can opt out per workflow / per workspace via Secrets, step inputs,
// or app config without having to remove the file.
func (a *Activator) Activate(_ context.Context) error {
	configcommon.LogCLIVersion(a.logger)
	a.logger.TInfof("Activate Bitrise mirrors for Gradle")

	gradleHome, err := a.resolveGradleHome()
	if err != nil {
		return err
	}

	// Skip the migration for an explicit GradleHome override; only heal env-resolved homes.
	if a.gradleHome == "" {
		if home, herr := a.osProxy.UserHomeDir(); herr == nil {
			if merr := mirrorsconfig.MigratePrebootInitScript(a.logger, a.osProxy, paths.FromHome(home).GradleHome(""), gradleHome); merr != nil {
				a.logger.Warnf("Could not relocate preboot Gradle mirrors init script: %s", merr)
			}
		}
	}

	enabled := a.resolveEnabled()
	datacenter := a.resolveDatacenter()
	selected := mirrorsconfig.FilterByFlagNames(a.selectedFlags)
	projectRoot := a.resolveProjectRoot()

	if err := mirrorsconfig.Activate(a.logger, a.osProxy, mirrorsconfig.Params{
		GradleHome:  gradleHome,
		Mirrors:     selected,
		Datacenter:  datacenter,
		Enabled:     enabled,
		ProjectRoot: projectRoot,
		Exporter:    envexport.New(a.envs, a.logger),
	}); err != nil {
		return fmt.Errorf("activate gradle mirrors: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Private — env fallback
// ---------------------------------------------------------------------------

func (a *Activator) resolveGradleHome() (string, error) {
	if a.gradleHome != "" {
		abs, err := a.pathModifier.AbsPath(a.gradleHome)
		if err != nil {
			return "", fmt.Errorf("expand Gradle home path (%s): %w", a.gradleHome, err)
		}

		return abs, nil
	}

	home, err := a.osProxy.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	return paths.FromHome(home).GradleHome(a.envs[paths.GradleUserHomeEnvKey]), nil
}

func (a *Activator) resolveEnabled() bool {
	if a.enabled != nil {
		return *a.enabled
	}

	return a.envs[mirrorsconfig.EnabledEnvKey] == "true"
}

func (a *Activator) resolveDatacenter() string {
	if a.datacenter != "" {
		return a.datacenter
	}

	return a.envs[mirrorsconfig.DatacenterEnvKey]
}

func (a *Activator) resolveProjectRoot() string {
	switch a.projectRoot {
	case "-":
		return ""
	case "":
		cwd, err := a.osProxy.Getwd()
		if err != nil {
			a.logger.Debugf("Could not determine working directory for scope-gap scan: %s", err)

			return ""
		}

		return cwd
	default:
		return a.projectRoot
	}
}
