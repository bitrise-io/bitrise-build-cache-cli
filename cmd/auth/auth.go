package auth

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
)

// nolint:gochecknoglobals
var authCmd = &cobra.Command{
	Use:          "auth",
	Short:        "Manage Bitrise Build Cache credentials stored in the OS keychain",
	Long:         `Manage Bitrise Build Cache credentials stored in the OS keychain (macOS Keychain, Linux secret-service). Credentials persisted here are used in preference to the BITRISE_BUILD_CACHE_AUTH_TOKEN / BITRISE_BUILD_CACHE_WORKSPACE_ID env vars.`,
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

		kc := keychain.New()
		if err := kc.Save(keychain.Credentials{
			AuthToken:   setToken,
			WorkspaceID: setWorkspaceID,
		}); err != nil {
			return fmt.Errorf("save credentials to keychain: %w", err)
		}

		logger.TInfof("✅ Credentials saved to the OS keychain")
		logger.Infof("You can now remove BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID from your shell rc files.")

		return nil
	},
}

// nolint:gochecknoglobals
var authGetCmd = &cobra.Command{
	Use:          "get",
	Short:        "Show whether Bitrise Build Cache credentials are stored in the OS keychain",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		creds, err := keychain.New().Load()
		if err != nil {
			if errors.Is(err, keychain.ErrNotFound) {
				logger.Warnf("No Bitrise Build Cache credentials stored in the OS keychain.")
				logger.Infof("Run `bitrise-build-cache auth set --token <token> --workspace-id <id>` to store them, or rely on env vars.")

				return nil
			}

			return fmt.Errorf("read credentials from keychain: %w", err)
		}

		logger.TInfof("Workspace ID: %s", creds.WorkspaceID)
		logger.TInfof("Auth token:   %s", maskToken(creds.AuthToken))

		return nil
	},
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

	common.RootCmd.AddCommand(authCmd)
}
