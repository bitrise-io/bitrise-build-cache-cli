package store

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
)

// PersistActivateCreds routes non-JWT activation creds to keychain (local) or the multiplatform Credentials field (CI); JWT keeps the legacy AuthConfig write for downstream reactnative/invocation compat.
func PersistActivateCreds(logger log.Logger, envs map[string]string, auth configcommon.CacheAuthConfig, mpCfg *multiplatformconfig.Config) {
	if auth.IsJWT {
		mpCfg.AuthConfig = auth

		return
	}
	target := SelectAuto(envs)
	if target.Kind() == KindFile {
		c := keychain.Credentials{AuthToken: auth.AuthToken, WorkspaceID: auth.WorkspaceID}
		mpCfg.Credentials = &c
		mpCfg.AuthConfig = auth
		logger.Infof("Saved auth credentials to the multiplatform config file (CI-safe — fastlane setup_ci swaps the keychain)")

		return
	}
	if err := target.Save(keychain.Credentials{AuthToken: auth.AuthToken, WorkspaceID: auth.WorkspaceID}); err != nil {
		logger.Warnf("Keychain save failed (%v); falling back to multiplatform authConfig", err)
		mpCfg.AuthConfig = auth

		return
	}
	logger.Infof("Saved auth credentials to the OS keychain")
}

// SetUsername writes name into the store that already holds credentials so a
// username-only edit can't strand an empty-token entry in the wrong backend.
// Empty name clears the override. Returns the store written to.
func SetUsername(envs map[string]string, name string) (Kind, error) {
	target, existing := storeHoldingCreds(envs)
	existing.Username = strings.TrimSpace(name)
	if err := target.Save(existing); err != nil {
		return target.Kind(), fmt.Errorf("save display name to %s: %w", target.Kind(), err)
	}

	return target.Kind(), nil
}

func storeHoldingCreds(envs map[string]string) (Store, keychain.Credentials) {
	for _, s := range []Store{NewKeychain(), NewFile()} {
		creds, err := s.Load()
		if err == nil && (strings.TrimSpace(creds.AuthToken) != "" || strings.TrimSpace(creds.WorkspaceID) != "") {
			return s, creds
		}
	}

	target := SelectAuto(envs)
	creds, _ := target.Load()

	return target, creds
}
