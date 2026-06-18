// Package reactnative provides a public API for the React Native build cache
// commands of bitrise-build-cache-cli.
package reactnative

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/gradle"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/multiplatform"
	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/dependencies"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/envexport"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ActivatorParams holds the activation flags.
type ActivatorParams struct {
	GradleEnabled bool
	XcodeEnabled  bool
	CppEnabled    bool
	DebugLogging  bool

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger
}

// Activator orchestrates Bitrise Build Cache activation for React Native.
type Activator struct {
	gradle       *gradleActivator
	xcode        *xcodeActivator
	cpp          *ccachepkg.Activator
	helper       *storageHelperStarter
	debugLogging bool
	logger       log.Logger
}

// NewActivator creates an Activator with production defaults.
func NewActivator(params ActivatorParams) *Activator {
	logger := params.Logger
	if logger == nil {
		logger = log.NewLogger(log.WithDebugLog(params.DebugLogging))
	}

	a := &Activator{
		debugLogging: params.DebugLogging,
		logger:       logger,
	}

	if params.GradleEnabled {
		a.gradle = &gradleActivator{logger: logger, debugLogging: params.DebugLogging}
	}

	if params.XcodeEnabled {
		a.xcode = &xcodeActivator{logger: logger, debugLogging: params.DebugLogging}
	}

	// ccache only meaningfully wraps the Android/Gradle native build path in
	// React Native projects — without Gradle activated there is nothing for
	// ccache to accelerate here. Tie the two together so `--cpp=true
	// --gradle=false` doesn't leave a stray ccache helper running.
	if params.CppEnabled && params.GradleEnabled {
		a.cpp = ccachepkg.NewActivator(ccachepkg.ActivatorParams{
			PushEnabled:  ccacheconfig.DefaultParams().PushEnabled,
			DebugLogging: params.DebugLogging,
			Logger:       logger,
		})
		a.helper = &storageHelperStarter{logger: logger}
	} else if params.CppEnabled && !params.GradleEnabled {
		logger.Infof("(i) Skipping C++ (ccache) activation: Gradle is disabled — ccache only wraps the Android/Gradle native build path.")
	}

	return a
}

// Activate runs the full React Native build cache activation flow:
// install dependencies → activate sub-systems → start storage helper → save config.
func (a *Activator) Activate(ctx context.Context) error {
	configcommon.LogCLIVersion(a.logger)
	a.logger.TInfof("Activate Bitrise Build Cache for React Native")

	if err := installDeps(ctx, a.logger, a.cpp != nil); err != nil {
		return fmt.Errorf("install dependencies: %w", err)
	}

	exportInstallDirToPath(a.logger) //nolint:contextcheck // envman export inside is fire-and-forget

	if a.gradle != nil {
		a.logger.TInfof("Activating Gradle build cache...")

		if err := a.gradle.activate(); err != nil {
			return fmt.Errorf("activate Gradle build cache: %w", err)
		}
	}

	if a.xcode != nil {
		a.logger.TInfof("Activating Xcode build cache...")

		if err := a.xcode.activate(ctx); err != nil {
			return fmt.Errorf("activate Xcode build cache: %w", err)
		}
	}

	if err := a.activateCppIfApplicable(ctx); err != nil {
		return err
	}

	a.exportEASWorkingDirIfCI() //nolint:contextcheck // envman export inside is fire-and-forget

	if err := saveMultiplatformConfig(a.debugLogging); err != nil {
		return err
	}

	if err := a.saveReactNativeMarker(); err != nil {
		return err
	}

	// Single consolidated benchmark phase summary across whichever sub-tools
	// were activated. Per-tool baseline warnings used to fire individually
	// from each ApplyBenchmarkPhase call, which made multi-tool RN runs look
	// like the whole build was in baseline even when only the *unused* tool
	// was (the relevant tool was caching normally).
	tools := []string{}
	if a.gradle != nil {
		tools = append(tools, configcommon.BuildToolGradle)
	}
	if a.xcode != nil {
		tools = append(tools, configcommon.BuildToolXcode)
	}
	configcommon.LogBenchmarkSummary(a.logger, tools)

	a.logger.TInfof("✅ Bitrise Build Cache for React Native activated")

	return nil
}

// activateCppIfApplicable activates ccache and starts the storage helper
// when ccache was wired in NewActivator AND gradle did not end up in the
// benchmark baseline phase. The gradle-baseline skip is a stop-gap until
// ccache grows its own benchmark phase support (ACI-4926) so the rotation
// stays consistent across both halves of the Android build.
func (a *Activator) activateCppIfApplicable(ctx context.Context) error {
	if a.cpp == nil {
		return nil
	}

	gradlePhase := configcommon.ReadBenchmarkPhaseFile(configcommon.BuildToolGradle, a.logger)
	if gradlePhase == configcommon.BenchmarkPhaseBaseline {
		a.logger.Infof("(i) Skipping C++ (ccache) activation: Gradle is in benchmark baseline mode.")

		return nil
	}

	a.logger.TInfof("Activating C++ build cache...")

	if err := a.cpp.Activate(ctx); err != nil {
		return fmt.Errorf("activate C++ build cache: %w", err)
	}

	if err := a.helper.start(); err != nil {
		return fmt.Errorf("start ccache storage helper: %w", err)
	}

	return nil
}

// exportEASWorkingDirIfCI pins EAS Build's working directory on CI so that
// `eas build --local` (Expo's local-build flow) reuses the same path across
// runs. Without a stable path, every cache that hashes absolute paths
// (Gradle, Xcode, ccache) misses. We only set it on CI — on a developer
// machine the Runner injects it on demand when it sees an `eas build`
// invocation, so we don't pollute the user's shell environment with an env
// var they may not need.
//
// An explicit user-supplied value always wins.
func (a *Activator) exportEASWorkingDirIfCI() {
	envs := utils.AllEnvs()
	if configcommon.DetectCIProvider(envs) == "" {
		return
	}

	if existing := envs[EASWorkingDirEnv]; existing != "" {
		a.logger.Debugf("%s already set to %q — leaving it alone", EASWorkingDirEnv, existing)

		return
	}

	workdir := DefaultEASWorkingDir(envs)
	envexport.New(envs, a.logger).Export(EASWorkingDirEnv, workdir)
	a.logger.TInfof("Exported %s=%s for EAS Build cache stability", EASWorkingDirEnv, workdir)
}

// saveReactNativeMarker writes ~/.bitrise/cache/reactnative/config.json to
// signal that RN build cache is active. The marker is what `status
// --feature=react-native` and external step integrations read to decide
// whether to wrap build commands with `react-native run --`.
func (a *Activator) saveReactNativeMarker() error {
	cfg := rnconfig.Config{Enabled: true}

	if err := cfg.Save(a.logger, utils.DefaultOsProxy{}, utils.DefaultEncoderFactory{}); err != nil {
		return fmt.Errorf("save react-native marker: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Private — sub-system activation
// ---------------------------------------------------------------------------

func exportInstallDirToPath(logger log.Logger) {
	dir := dependencies.InstallDir
	envs := utils.AllEnvs()

	currentPath := envs["PATH"]
	if strings.Contains(string(os.PathListSeparator)+currentPath+string(os.PathListSeparator), string(os.PathListSeparator)+dir+string(os.PathListSeparator)) {
		return
	}

	newPath := dir + ":" + currentPath
	exporter := envexport.New(envs, logger)
	exporter.Export("PATH", newPath)

	logger.TInfof("Added %s to PATH", dir)
}

func installDeps(ctx context.Context, logger log.Logger, doCpp bool) error {
	var tools []dependencies.Tool

	cliTool, err := dependencies.CLITool()
	if err != nil {
		return fmt.Errorf("determine CLI tool: %w", err)
	}

	tools = append(tools, cliTool)

	if doCpp {
		tools = append(tools, dependencies.CcacheTool())
	}

	if err := dependencies.EnsureAll(ctx, tools, logger); err != nil {
		return fmt.Errorf("ensure dependencies: %w", err)
	}

	return nil
}

type gradleActivator struct {
	logger       log.Logger
	debugLogging bool
}

func (g *gradleActivator) activate() error {
	gradleHome, err := pathutil.NewPathModifier().AbsPath("~/.gradle")
	if err != nil {
		return fmt.Errorf("expand Gradle home path: %w", err)
	}

	gradleParams := gradleconfig.DefaultActivateGradleParams()
	gradleParams.Cache.Enabled = true
	gradleParams.Cache.PushEnabled = true

	if err := gradleconfig.Activate(
		g.logger,
		gradleHome,
		utils.AllEnvs(),
		g.debugLogging,
		gradleParams.TemplateInventory,
		func(inventory gradleconfig.TemplateInventory, path string) error {
			return inventory.WriteToGradleInit(
				g.logger,
				path,
				utils.DefaultOsProxy{},
				gradleconfig.GradleTemplateProxy(),
			)
		},
		gradleconfig.DefaultGradlePropertiesUpdater(),
		gradleParams,
	); err != nil {
		return fmt.Errorf("gradle activation: %w", err)
	}

	return nil
}

type xcodeActivator struct {
	logger       log.Logger
	debugLogging bool
}

func (x *xcodeActivator) activate(ctx context.Context) error {
	xcodeParams := xcelerate.DefaultParams()
	xcodeParams.DebugLogging = x.debugLogging

	if err := xcelerate.Activate(
		ctx,
		x.logger,
		utils.DefaultOsProxy{},
		utils.DefaultCommandFunc(),
		utils.DefaultEncoderFactory{},
		utils.DefaultDecoderFactory{},
		xcodeParams,
		utils.AllEnvs(),
	); err != nil {
		return fmt.Errorf("xcode activation: %w", err)
	}

	return nil
}

type storageHelperStarter struct {
	logger log.Logger
}

func (s *storageHelperStarter) start() error {
	s.logger.TInfof("Starting ccache storage helper...")

	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	cmd := exec.Command(binary, "ccache", "storage-helper", "start") //nolint:gosec,noctx // intentionally detached: the helper must outlive this command
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start storage helper process: %w", err)
	}

	s.logger.TInfof("Ccache storage helper started (pid %d)", cmd.Process.Pid)

	return nil
}

func saveMultiplatformConfig(debugLogging bool) error {
	authConfig, err := configcommon.ReadAuthConfigFromEnvironments(utils.AllEnvs())
	if err != nil {
		return fmt.Errorf("read auth config for multiplatform analytics: %w", err)
	}

	cfg := multiplatformconfig.Config{
		AuthConfig:   authConfig,
		DebugLogging: debugLogging,
	}

	if err := cfg.Save(utils.DefaultOsProxy{}, utils.DefaultEncoderFactory{}); err != nil {
		return fmt.Errorf("save multiplatform analytics config: %w", err)
	}

	return nil
}
