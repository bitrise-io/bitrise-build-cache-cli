package common

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/bazel"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

//nolint:gochecknoglobals
var interactiveFlag bool

const (
	envWorkspaceID = "BITRISE_BUILD_CACHE_WORKSPACE_ID"
	envAuthToken   = "BITRISE_BUILD_CACHE_AUTH_TOKEN" //nolint:gosec // env-var key, not a credential
)

type interactiveTool string

const (
	toolGradle interactiveTool = "gradle"
	toolBazel  interactiveTool = "bazel"
	toolXcode  interactiveTool = "xcode"
	toolCcache interactiveTool = "ccache"
)

// prompter shares one bufio.Reader across prompts — separate readers would buffer ahead and drop piped input.
type prompter struct {
	reader       *bufio.Reader
	out          io.Writer
	readPassword func() (string, error)
}

func newDefaultPrompter() *prompter {
	stdinReader := bufio.NewReader(os.Stdin)

	return &prompter{
		reader: stdinReader,
		out:    os.Stdout,
		readPassword: func() (string, error) {
			fd := int(os.Stdin.Fd())
			if !term.IsTerminal(fd) {
				line, err := stdinReader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return "", fmt.Errorf("read auth token: %w", err)
				}

				return strings.TrimRight(line, "\r\n"), nil
			}

			b, err := term.ReadPassword(fd)
			if err != nil {
				return "", fmt.Errorf("read masked input: %w", err)
			}

			return string(b), nil
		},
	}
}

func init() { //nolint:gochecknoinits
	ActivateCmd.Flags().BoolVar(&interactiveFlag, "interactive", false,
		"Launch an interactive guided local setup. Prompts for the tool and credentials instead of reading them from environment variables.")
	ActivateCmd.SilenceUsage = true
	ActivateCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if !interactiveFlag {
			return cmd.Help() //nolint:wrapcheck // help has no useful error to wrap
		}

		if term.IsTerminal(int(os.Stdin.Fd())) {
			return runInteractiveSetupHuh(cmd.Context())
		}

		return runInteractiveSetup(cmd.Context(), newDefaultPrompter())
	}
}

func runInteractiveSetupHuh(ctx context.Context) error {
	logger := log.NewLogger()
	logger.EnableDebugLog(IsDebugLogMode)
	logger.TInfof("Bitrise Build Cache - interactive local setup")

	kc := keychain.New()

	startWS, startToken, source := loadStartingCredentials(kc, os.Getenv(envWorkspaceID), os.Getenv(envAuthToken))

	var (
		selectedTools []string
		workspaceID   = startWS
		authToken     = startToken
		pushEnabled   bool
	)

	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Which build tools should I set up?").
				Description("Use space to toggle, enter to confirm.").
				Options(
					huh.NewOption("Gradle", string(toolGradle)),
					huh.NewOption("Bazel", string(toolBazel)),
					huh.NewOption("Xcode", string(toolXcode)),
					huh.NewOption("ccache (C/C++)", string(toolCcache)),
				).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return errors.New("pick at least one tool")
					}

					return nil
				}).
				Value(&selectedTools),
		),
	}

	if source == credsSourceNone {
		groups = append(groups,
			huh.NewGroup(
				huh.NewInput().
					Title("Workspace ID").
					Description("Find it at https://app.bitrise.io").
					Validate(nonEmpty("Workspace ID")).
					Value(&workspaceID),
				huh.NewInput().
					Title("Auth token").
					Description("Personal access token. Input is hidden.").
					EchoMode(huh.EchoModePassword).
					Validate(nonEmpty("Auth token")).
					Value(&authToken),
			),
		)
	}

	groups = append(groups,
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable cache push?").
				Description("Default off — recommended for local dev (so a flaky local build can't poison the shared cache).").
				Affirmative("Yes, push too").
				Negative("No, pull only").
				Value(&pushEnabled),
		),
	)

	if err := huh.NewForm(groups...).Run(); err != nil {
		return fmt.Errorf("interactive wizard: %w", err)
	}

	switch source {
	case credsSourceKeychain:
		logger.TInfof("Using credentials from the OS keychain.")
	case credsSourceEnv:
		logger.TInfof("Imported BITRISE_BUILD_CACHE_AUTH_TOKEN + WORKSPACE_ID from env into the OS keychain.")
		logger.Infof("You can now remove them from your shell rc files.")
		_ = persistCredentials(kc, workspaceID, authToken)
	case credsSourceNone:
		_ = persistCredentials(kc, workspaceID, authToken)
		logger.TInfof("Credentials saved to the OS keychain. Future runs will pick them up automatically.")
	}

	envs := utils.AllEnvs()
	envs[envWorkspaceID] = workspaceID
	envs[envAuthToken] = authToken

	return runSelectedTools(ctx, logger, selectedTools, envs, pushEnabled)
}

func nonEmpty(label string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(label + " cannot be empty")
		}

		return nil
	}
}

func runSelectedTools(ctx context.Context, logger log.Logger, tools []string, envs map[string]string, pushEnabled bool) error {
	for _, t := range tools {
		var err error
		switch interactiveTool(t) {
		case toolGradle:
			err = runInteractiveGradle(logger, envs, pushEnabled)
		case toolBazel:
			err = runInteractiveBazel(logger, envs, pushEnabled)
		case toolXcode:
			err = runInteractiveXcode(ctx, logger, envs, pushEnabled)
		case toolCcache:
			err = runInteractiveCcache(ctx, logger, envs, pushEnabled)
		default:
			err = fmt.Errorf("unsupported tool: %s", t)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

type credsSource int

const (
	credsSourceNone credsSource = iota
	credsSourceKeychain
	credsSourceEnv
)

func loadStartingCredentials(kc keychainStore, envWS, envToken string) (string, string, credsSource) {
	if creds, err := kc.Load(); err == nil && creds.AuthToken != "" && creds.WorkspaceID != "" {
		return creds.WorkspaceID, creds.AuthToken, credsSourceKeychain
	}

	if envToken != "" && envWS != "" {
		return envWS, envToken, credsSourceEnv
	}

	return "", "", credsSourceNone
}

func persistCredentials(kc keychainStore, workspaceID, authToken string) error {
	if err := kc.Save(keychain.Credentials{AuthToken: authToken, WorkspaceID: workspaceID}); err != nil {
		return fmt.Errorf("save credentials to keychain: %w", err)
	}

	return nil
}

func runInteractiveSetup(ctx context.Context, p *prompter) error {
	logger := log.NewLogger()
	logger.EnableDebugLog(IsDebugLogMode)
	logger.TInfof("Bitrise Build Cache - interactive local setup")

	fmt.Fprintln(p.out)
	fmt.Fprintln(p.out, "This wizard will configure Bitrise Build Cache for a build tool on this machine.")
	fmt.Fprintln(p.out, "You can find your workspace ID and a personal access token at: https://app.bitrise.io")
	fmt.Fprintln(p.out)

	tool, err := promptTool(p)
	if err != nil {
		return err
	}

	workspaceID, authToken, err := resolveCredentials(p, keychain.New())
	if err != nil {
		return err
	}

	pushEnabled, err := promptPushEnabled(p)
	if err != nil {
		return err
	}

	envs := utils.AllEnvs()
	envs[envWorkspaceID] = workspaceID
	envs[envAuthToken] = authToken

	switch tool {
	case toolGradle:
		return runInteractiveGradle(logger, envs, pushEnabled)
	case toolBazel:
		return runInteractiveBazel(logger, envs, pushEnabled)
	case toolXcode:
		return runInteractiveXcode(ctx, logger, envs, pushEnabled)
	case toolCcache:
		return runInteractiveCcache(ctx, logger, envs, pushEnabled)
	default:
		return fmt.Errorf("unsupported tool: %s", tool)
	}
}

type keychainStore interface {
	Load() (keychain.Credentials, error)
	Save(creds keychain.Credentials) error
}

func resolveCredentials(p *prompter, kc keychainStore) (string, string, error) {
	creds, err := kc.Load()
	if err == nil && creds.AuthToken != "" && creds.WorkspaceID != "" {
		fmt.Fprintln(p.out, "Reusing credentials already stored in the OS keychain.")

		return creds.WorkspaceID, creds.AuthToken, nil
	}

	envToken := os.Getenv(envAuthToken)
	envWS := os.Getenv(envWorkspaceID)

	if envToken != "" && envWS != "" {
		fmt.Fprintln(p.out)
		fmt.Fprintln(p.out, "Found BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID in env.")
		fmt.Fprintln(p.out, "Importing them into the OS keychain so you can remove them from your shell rc files.")

		if err := kc.Save(keychain.Credentials{AuthToken: envToken, WorkspaceID: envWS}); err != nil {
			fmt.Fprintf(p.out, "Could not save to keychain (%v). Continuing with env values for this run only.\n", err)

			return envWS, envToken, nil
		}

		fmt.Fprintln(p.out, "✅ Credentials saved to the OS keychain.")
		fmt.Fprintln(p.out, "You can now remove BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID from your shell rc files.")

		return envWS, envToken, nil
	}

	workspaceID, err := promptRequiredLine(p, "Workspace ID")
	if err != nil {
		return "", "", err
	}

	authToken, err := promptRequiredSecret(p, "Auth token (input hidden)")
	if err != nil {
		return "", "", err
	}

	if err := kc.Save(keychain.Credentials{AuthToken: authToken, WorkspaceID: workspaceID}); err != nil {
		fmt.Fprintf(p.out, "Could not save credentials to the OS keychain (%v). Continuing with values for this run only.\n", err)
	} else {
		fmt.Fprintln(p.out, "✅ Credentials saved to the OS keychain. Future CLI runs will pick them up automatically.")
	}

	return workspaceID, authToken, nil
}

func promptPushEnabled(p *prompter) (bool, error) {
	fmt.Fprintln(p.out)
	fmt.Fprintln(p.out, "Cache access mode:")
	fmt.Fprintln(p.out, "  1) Pull only (recommended for local dev)")
	fmt.Fprintln(p.out, "  2) Pull and push")

	for {
		fmt.Fprint(p.out, "Enter 1-2 [default: 1]: ")

		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("read cache access mode: %w", err)
		}

		switch strings.TrimSpace(line) {
		case "", "1":
			return false, nil
		case "2":
			return true, nil
		}

		fmt.Fprintln(p.out, "Invalid selection. Please enter 1 or 2.")

		if errors.Is(err, io.EOF) {
			return false, errors.New("no cache access mode selected (stdin closed)")
		}
	}
}

func promptTool(p *prompter) (interactiveTool, error) {
	tools := []interactiveTool{toolGradle, toolBazel, toolXcode, toolCcache}

	fmt.Fprintln(p.out, "Which build tool would you like to set up?")

	for i, t := range tools {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, t)
	}

	for {
		fmt.Fprintf(p.out, "Enter 1-%d: ", len(tools))

		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read tool selection: %w", err)
		}

		line = strings.TrimSpace(line)
		idx, convErr := strconv.Atoi(line)

		if convErr != nil || idx < 1 || idx > len(tools) {
			fmt.Fprintf(p.out, "Invalid selection %q. Please enter a number between 1 and %d.\n", line, len(tools))

			if errors.Is(err, io.EOF) {
				return "", errors.New("no tool selected (stdin closed)")
			}

			continue
		}

		return tools[idx-1], nil
	}
}

func promptRequiredLine(p *prompter, label string) (string, error) {
	for {
		fmt.Fprintf(p.out, "%s: ", label)

		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read %s: %w", label, err)
		}

		value := strings.TrimSpace(line)
		if value != "" {
			return value, nil
		}

		fmt.Fprintf(p.out, "%s cannot be empty.\n", label)

		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("%s not provided (stdin closed)", label)
		}
	}
}

func promptRequiredSecret(p *prompter, label string) (string, error) {
	for {
		fmt.Fprintf(p.out, "%s: ", label)

		value, err := p.readPassword()
		fmt.Fprintln(p.out)

		if err != nil {
			return "", err
		}

		value = strings.TrimSpace(value)
		if value != "" {
			return value, nil
		}

		fmt.Fprintf(p.out, "%s cannot be empty.\n", label)
	}
}

func runInteractiveGradle(logger log.Logger, envs map[string]string, pushEnabled bool) error {
	gradleHome, err := pathutil.NewPathModifier().AbsPath("~/.gradle")
	if err != nil {
		return fmt.Errorf("expand Gradle home path: %w", err)
	}

	params := gradleconfig.DefaultActivateGradleParams()
	params.Cache.PushEnabled = pushEnabled

	if err := gradleconfig.Activate(
		logger,
		gradleHome,
		envs,
		IsDebugLogMode,
		params.TemplateInventory,
		func(inventory gradleconfig.TemplateInventory, path string) error {
			return inventory.WriteToGradleInit(
				logger,
				path,
				utils.DefaultOsProxy{},
				gradleconfig.GradleTemplateProxy(),
			)
		},
		gradleconfig.DefaultGradlePropertiesUpdater(),
		params,
	); err != nil {
		return fmt.Errorf("activate plugins for Gradle: %w", err)
	}

	logger.TInfof("✅ Bitrise Build Cache activated for Gradle")

	return nil
}

func runInteractiveBazel(logger log.Logger, envs map[string]string, pushEnabled bool) error {
	homeDir, err := pathutil.NewPathModifier().AbsPath("~")
	if err != nil {
		return fmt.Errorf("expand home path: %w", err)
	}

	bazelrcPath := filepath.Join(homeDir, ".bazelrc")
	params := bazelconfig.DefaultActivateBazelParams()
	params.Cache.PushEnabled = pushEnabled

	commandFunc := func(cmd string, args ...string) (string, error) {
		out, err2 := exec.Command(cmd, args...).CombinedOutput() //nolint:noctx
		if err2 == nil {
			return string(out), nil
		}

		return string(out), fmt.Errorf("run cmd: %w", err2)
	}

	inventory, err := params.TemplateInventory(logger, envs, commandFunc, IsDebugLogMode)
	if err != nil {
		return fmt.Errorf("build Bazel template inventory: %w", err)
	}

	if err := inventory.WriteToBazelrc(logger, bazelrcPath, utils.DefaultOsProxy{}, utils.DefaultTemplateProxy()); err != nil {
		return fmt.Errorf("write .bazelrc: %w", err)
	}

	logger.TInfof("✅ Bitrise Build Cache activated for Bazel")

	return nil
}

func runInteractiveCcache(ctx context.Context, logger log.Logger, envs map[string]string, pushEnabled bool) error {
	activator := ccachepkg.NewActivator(ccachepkg.ActivatorParams{
		PushEnabled:  pushEnabled,
		DebugLogging: IsDebugLogMode,
		Envs:         envs,
		Logger:       logger,
	})

	if err := activator.Activate(ctx); err != nil {
		return fmt.Errorf("activate Bitrise Build Cache for ccache: %w", err)
	}

	return nil
}

func runInteractiveXcode(ctx context.Context, logger log.Logger, envs map[string]string, pushEnabled bool) error {
	params := xcelerate.DefaultParams()
	params.DebugLogging = IsDebugLogMode
	params.PushEnabled = pushEnabled

	if err := xcelerate.Activate(
		ctx,
		logger,
		utils.DefaultOsProxy{},
		utils.DefaultCommandFunc(),
		utils.DefaultEncoderFactory{},
		utils.DefaultDecoderFactory{},
		params,
		envs,
	); err != nil {
		return fmt.Errorf("activate Bitrise Build Cache for Xcode: %w", err)
	}

	logger.TInfof("✅ Bitrise Build Cache activated for Xcode")

	return nil
}
