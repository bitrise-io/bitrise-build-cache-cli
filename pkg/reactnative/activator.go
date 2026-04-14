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
}

// Activator orchestrates Bitrise Build Cache activation for React Native.
type Activator struct {
	Params ActivatorParams
	Logger log.Logger
}

// Activate runs the full React Native build cache activation flow:
// install dependencies → activate sub-systems → start storage helper → save config.
func (a *Activator) Activate(ctx context.Context) error {
	logger := a.Logger
	if logger == nil {
		logger = log.NewLogger(log.WithDebugLog(a.Params.DebugLogging))
	}

	logger.TInfof("Activate Bitrise Build Cache for React Native")

	if err := installDeps(ctx, logger, a.Params.Cpp); err != nil {
		return fmt.Errorf("install dependencies: %w", err)
	}

	exportInstallDirToPath(logger)

	if a.Params.Gradle {
		logger.TInfof("Activating Gradle build cache...")

		if err := activateGradle(logger, a.Params.DebugLogging); err != nil {
			return fmt.Errorf("activate Gradle build cache: %w", err)
		}
	}

	if a.Params.Xcode {
		logger.TInfof("Activating Xcode build cache...")

		if err := activateXcode(ctx, logger, a.Params.DebugLogging); err != nil {
			return fmt.Errorf("activate Xcode build cache: %w", err)
		}
	}

	if a.Params.Cpp {
		logger.TInfof("Activating C++ build cache...")

		if err := activateCpp(ctx, logger, a.Params.DebugLogging); err != nil {
			return fmt.Errorf("activate C++ build cache: %w", err)
		}

		logger.TInfof("Starting ccache storage helper...")

		if err := startStorageHelper(logger); err != nil {
			return fmt.Errorf("start ccache storage helper: %w", err)
		}
	}

	if err := saveMultiplatformConfig(a.Params.DebugLogging); err != nil {
		return err
	}

	logger.TInfof("✅ Bitrise Build Cache for React Native activated")

	return nil
}

// ---------------------------------------------------------------------------
// Private — sub-system activation
// ---------------------------------------------------------------------------

func exportInstallDirToPath(logger log.Logger) {
	dir := dependencies.InstallDir
	envs := utils.AllEnvs()

	currentPath := envs["PATH"]
	if strings.Contains(currentPath, dir) {
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

func activateGradle(logger log.Logger, debugLogging bool) error {
	gradleHome, err := pathutil.NewPathModifier().AbsPath("~/.gradle")
	if err != nil {
		return fmt.Errorf("expand Gradle home path: %w", err)
	}

	gradleParams := gradleconfig.DefaultActivateGradleParams()
	gradleParams.Cache.Enabled = true
	gradleParams.Cache.PushEnabled = true

	if err := gradleconfig.Activate(
		logger,
		gradleHome,
		utils.AllEnvs(),
		debugLogging,
		gradleParams.TemplateInventory,
		func(inventory gradleconfig.TemplateInventory, path string) error {
			return inventory.WriteToGradleInit(
				logger,
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

func activateXcode(ctx context.Context, logger log.Logger, debugLogging bool) error {
	xcodeParams := xcelerate.DefaultParams()
	xcodeParams.DebugLogging = debugLogging

	if err := xcelerate.Activate(
		ctx,
		logger,
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

func activateCpp(ctx context.Context, logger log.Logger, debugLogging bool) error {
	a := ccachepkg.NewActivator(ccachepkg.ActivatorParams{
		PushEnabled:  ccacheconfig.DefaultParams().PushEnabled,
		DebugLogging: debugLogging,
	})
	a.Logger = logger

	if err := a.Activate(ctx); err != nil {
		return fmt.Errorf("cpp activation: %w", err)
	}

	return nil
}

func startStorageHelper(logger log.Logger) error {
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

	logger.TInfof("Ccache storage helper started (pid %d)", cmd.Process.Pid)

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
