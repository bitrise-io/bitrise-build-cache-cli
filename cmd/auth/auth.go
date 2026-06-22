package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// nolint:gochecknoglobals
var authCmd = &cobra.Command{
	Use:          "auth",
	Short:        "Manage Bitrise Build Cache credentials stored in the OS keychain",
	Long:         `Manage Bitrise Build Cache credentials stored in the OS keychain (macOS Keychain, Linux secret-service). Stored credentials are used when BITRISE_BUILD_CACHE_AUTH_TOKEN / BITRISE_BUILD_CACHE_WORKSPACE_ID (or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN on Bitrise CI) are not set — env vars take precedence so you can override the stored credentials for a single run.`,
	SilenceUsage: true,
}

// nolint:gochecknoglobals
var (
	setToken       string
	setWorkspaceID string
)

// nolint:gochecknoglobals
var authSetCmd = &cobra.Command{
	Use:          "set",
	Short:        "Store Bitrise Build Cache credentials in the OS keychain",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		setToken = strings.TrimSpace(setToken)
		setWorkspaceID = strings.TrimSpace(setWorkspaceID)

		switch {
		case setToken == "" && setWorkspaceID == "":
			return errors.New("--token and --workspace-id are required and must not be empty")
		case setToken == "":
			return errors.New("--token is required and must not be empty")
		case setWorkspaceID == "":
			return errors.New("--workspace-id is required and must not be empty")
		}

		kc := keychain.New()
		if err := kc.Save(keychain.Credentials{
			AuthToken:   setToken,
			WorkspaceID: setWorkspaceID,
		}); err != nil {
			return fmt.Errorf("save credentials to keychain: %w", err)
		}

		logger.TInfof("✅ Credentials saved to the OS keychain")

		switch scrubbed, err := scrubDiskCredentials(); {
		case err != nil:
			logger.Warnf("Saved to keychain, but could not strip plain-text credentials from disk: %v", err)
			logger.Warnf("Run `bitrise-build-cache auth get` to audit remaining sources.")
		case len(scrubbed) > 0:
			logger.TInfof("Scrubbed plain-text credentials from %s", strings.Join(scrubbed, ", "))
		}

		logger.Infof("You can now remove BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID from your shell rc files.")
		logger.Infof("If you have running Gradle daemons, stop them so the new token is picked up: `./gradlew --stop`.")

		return nil
	},
}

func scrubDiskCredentials() ([]string, error) {
	osProxy := utils.DefaultOsProxy{}

	cfg, err := multiplatformconfig.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read multiplatform config: %w", err)
	}

	if cfg.AuthConfig.AuthToken == "" && cfg.AuthConfig.WorkspaceID == "" {
		return nil, nil
	}

	cfg.AuthConfig = configcommon.CacheAuthConfig{}
	if err := cfg.Save(osProxy, utils.DefaultEncoderFactory{}); err != nil {
		return nil, fmt.Errorf("save scrubbed multiplatform config: %w", err)
	}

	return []string{"~/.bitrise/analytics/multiplatform/config.json"}, nil
}

// nolint:gochecknoglobals
var authGetCmd = &cobra.Command{
	Use:          "get",
	Short:        "Show Bitrise Build Cache credentials discovered across all known sources",
	Long:         "Lists credentials found in the OS keychain, the multiplatform analytics config on disk, and the BITRISE_BUILD_CACHE_AUTH_TOKEN / BITRISE_BUILD_CACHE_WORKSPACE_ID / BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN env vars. Use this to audit where your credentials live and to migrate them to the OS keychain.",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		sources := credSources(utils.AllEnvs())

		var keychainPopulated, foundElsewhere bool
		for i, s := range sources {
			populated := printSource(logger, s)
			switch {
			case i == 0:
				keychainPopulated = populated
			case populated:
				foundElsewhere = true
			}
		}

		if !keychainPopulated && foundElsewhere {
			logger.Println()
			logger.Infof("Credentials exist outside the OS keychain — migrate with:")
			logger.Infof("  bitrise-build-cache auth set --token <token> --workspace-id <workspace-id>")
			logger.Infof("`auth set` scrubs the multiplatform config in place; xcelerate and ccache configs are re-written cleanly on the next `activate xcode` / `activate c++`.")
		}

		return nil
	},
}

type credSourceState int

const (
	sourceAbsent credSourceState = iota
	sourcePartial
	sourceReadError
	sourcePopulated
	sourcePopulatedTokenOnly
)

type credAudit struct {
	state       credSourceState
	workspaceID string
	authToken   string
	note        string
	err         error
}

type credSource struct {
	label    string
	location string
	probe    func() credAudit
}

// First entry MUST be the keychain — RunE treats sources[0] as the target.
func credSources(envs map[string]string) []credSource {
	return []credSource{
		{"OS keychain", "<system keychain>", probeKeychain},
		{"Multiplatform config", "~/.bitrise/analytics/multiplatform/config.json", probeMultiplatform},
		{"Xcelerate config", "~/.bitrise-xcelerate/config.json", probeRawConfig("~/.bitrise-xcelerate/config.json")},
		{"Ccache config", "~/.bitrise/cache/ccache/config.json", probeRawConfig("~/.bitrise/cache/ccache/config.json")},
		{"Env vars (BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID)", "process env", func() credAudit { return probeEnvVars(envs) }},
		{"CI JWT (BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN)", "process env", func() credAudit { return probeJWT(envs) }},
	}
}

func printSource(logger log.Logger, s credSource) bool {
	if s.location != "" {
		logger.TInfof("%s (%s):", s.label, s.location)
	} else {
		logger.TInfof("%s:", s.label)
	}

	a := s.probe()
	switch a.state {
	case sourceAbsent:
		if a.note != "" {
			logger.Infof("  %s", a.note)
		} else {
			logger.Infof("  not configured")
		}

		return false
	case sourcePartial:
		logger.Warnf("  partial: %s", a.note)

		return false
	case sourceReadError:
		logger.Errorf("  read failed: %v", a.err)

		return false
	case sourcePopulated:
		logger.Infof("  Workspace ID: %s", a.workspaceID)
		logger.Infof("  Auth token:   %s", maskToken(a.authToken))

		return true
	case sourcePopulatedTokenOnly:
		logger.Infof("  set (%s)", maskToken(a.authToken))

		return true
	}

	return false
}

func probeKeychain() credAudit {
	creds, err := keychain.New().Load()
	switch {
	case errors.Is(err, keychain.ErrNotFound):
		return credAudit{state: sourceAbsent}
	case err != nil:
		return credAudit{state: sourceReadError, err: err}
	}

	return credAudit{state: sourcePopulated, workspaceID: creds.WorkspaceID, authToken: creds.AuthToken}
}

func probeMultiplatform() credAudit {
	cfg, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return credAudit{state: sourceAbsent, note: "not present"}
	case err != nil:
		return credAudit{state: sourceReadError, err: err}
	case cfg.AuthConfig.AuthToken == "":
		return credAudit{state: sourceAbsent, note: "present but no credentials"}
	}

	return credAudit{state: sourcePopulated, workspaceID: cfg.AuthConfig.WorkspaceID, authToken: cfg.AuthConfig.AuthToken}
}

// probeRawConfig reads the file directly — the per-tool ReadConfig overlays multiplatform credentials, which would mask the actual on-disk content this audit needs to see.
func probeRawConfig(displayPath string) func() credAudit {
	return func() credAudit {
		home, err := utils.DefaultOsProxy{}.UserHomeDir()
		if err != nil {
			return credAudit{state: sourceReadError, err: fmt.Errorf("resolve home dir: %w", err)}
		}

		fullPath := filepath.Join(home, strings.TrimPrefix(displayPath, "~/"))

		body, err := os.ReadFile(fullPath) //nolint:gosec // path composed from home + constant
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return credAudit{state: sourceAbsent, note: "not present"}
		case err != nil:
			return credAudit{state: sourceReadError, err: err}
		}

		var raw struct {
			AuthConfig struct {
				AuthToken   string `json:"authToken"`
				WorkspaceID string `json:"workspaceID"`
			} `json:"authConfig"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return credAudit{state: sourceReadError, err: fmt.Errorf("decode: %w", err)}
		}

		if raw.AuthConfig.AuthToken == "" {
			return credAudit{state: sourceAbsent, note: "present but no embedded credentials"}
		}

		return credAudit{state: sourcePopulated, workspaceID: raw.AuthConfig.WorkspaceID, authToken: raw.AuthConfig.AuthToken}
	}
}

func probeEnvVars(envs map[string]string) credAudit {
	tok := envs["BITRISE_BUILD_CACHE_AUTH_TOKEN"]
	ws := envs["BITRISE_BUILD_CACHE_WORKSPACE_ID"]

	switch {
	case tok != "" && ws != "":
		return credAudit{state: sourcePopulated, workspaceID: ws, authToken: tok}
	case tok != "" || ws != "":
		return credAudit{state: sourcePartial, note: "only one of AUTH_TOKEN / WORKSPACE_ID is set"}
	}

	return credAudit{state: sourceAbsent, note: "not set"}
}

func probeJWT(envs map[string]string) credAudit {
	jwt := envs["BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN"]
	if jwt == "" {
		return credAudit{state: sourceAbsent, note: "not set"}
	}

	return credAudit{state: sourcePopulatedTokenOnly, authToken: jwt}
}

// nolint:gochecknoglobals
var authClearCmd = &cobra.Command{
	Use:          "clear",
	Short:        "Remove Bitrise Build Cache credentials from the OS keychain",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		if err := keychain.New().Clear(); err != nil {
			return fmt.Errorf("clear credentials from keychain: %w", err)
		}

		logger.TInfof("✅ Credentials removed from the OS keychain")

		return nil
	},
}

// nolint:gochecknoglobals
var authTokenCmd = &cobra.Command{
	Use:           "token",
	Short:         "Resolve and print the Bitrise Build Cache auth token to stdout",
	Long:          "Resolves the auth token via the same precedence chain as the rest of the CLI (env vars → OS keychain → multiplatform analytics config) and prints it to stdout. Intended for build-time consumers (Gradle init script, future Bazel workspace_status_command) that need the resolved token without baking it into a config file. On failure exits non-zero with a short one-line message on stderr (no cobra Error: prefix) — callers framing the wrapper script own the wording.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := configcommon.ResolveAuthConfig(utils.AllEnvs())
		if err != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err.Error())

			return fmt.Errorf("resolve auth config: %w", err)
		}

		if _, err := fmt.Fprintln(cmd.OutOrStdout(), cfg.AuthToken); err != nil {
			return fmt.Errorf("write auth token: %w", err)
		}

		return nil
	},
}

func maskToken(token string) string {
	const tailLen = 4
	if len(token) <= tailLen {
		return "(present, length too short to mask)"
	}

	return fmt.Sprintf("****%s", token[len(token)-tailLen:])
}

func init() {
	authSetCmd.Flags().StringVar(&setToken, "token", "", "Bitrise Build Cache auth token (required)")
	authSetCmd.Flags().StringVar(&setWorkspaceID, "workspace-id", "", "Bitrise workspace ID (required)")
	_ = authSetCmd.MarkFlagRequired("token")
	_ = authSetCmd.MarkFlagRequired("workspace-id")

	authCmd.AddCommand(authSetCmd)
	authCmd.AddCommand(authGetCmd)
	authCmd.AddCommand(authClearCmd)
	authCmd.AddCommand(authTokenCmd)

	common.RootCmd.AddCommand(authCmd)
}
