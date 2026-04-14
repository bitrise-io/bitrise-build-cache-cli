// Package reactnative provides a public API for the React Native build cache
// commands of bitrise-build-cache-cli.
package reactnative

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/v2/log"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/dependencies"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/envexport"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/pkg/ccache"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ActivatorParams holds the activation flags.
type ActivatorParams struct {
	Gradle       bool
	Xcode        bool
	Cpp          bool
	DebugLogging bool

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

	if params.Gradle {
		a.gradle = &gradleActivator{logger: logger, debugLogging: params.DebugLogging}
	}

	if params.Xcode {
		a.xcode = &xcodeActivator{logger: logger, debugLogging: params.DebugLogging}
	}

	if params.Cpp {
		a.cpp = ccachepkg.NewActivator(ccachepkg.ActivatorParams{
			PushEnabled:  ccacheconfig.DefaultParams().PushEnabled,
			DebugLogging: params.DebugLogging,
			Logger:       logger,
		})
		a.helper = &storageHelperStarter{logger: logger}
	}

	return a
}

// Activate runs the full React Native build cache activation flow:
// install dependencies → activate sub-systems → start storage helper → save config.
func (a *Activator) Activate(ctx context.Context) error {
	a.logger.TInfof("Activate Bitrise Build Cache for React Native")

	if err := installDeps(ctx, a.logger, a.cpp != nil); err != nil {
		return fmt.Errorf("install dependencies: %w", err)
	}

	exportInstallDirToPath(a.logger)

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

	if a.cpp != nil {
		a.logger.TInfof("Activating C++ build cache...")

		if err := a.cpp.Activate(ctx); err != nil {
			return fmt.Errorf("activate C++ build cache: %w", err)
		}

		if err := a.helper.start(); err != nil {
			return fmt.Errorf("start ccache storage helper: %w", err)
		}
	}

	if err := saveMultiplatformConfig(a.debugLogging); err != nil {
		return err
	}

	a.logger.TInfof("✅ Bitrise Build Cache for React Native activated")

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
