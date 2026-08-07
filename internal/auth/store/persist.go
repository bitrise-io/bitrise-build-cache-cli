package store

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
)

// PersistActivateCreds routes non-JWT activation creds to keychain (local) or the multiplatform Credentials field (CI); JWT keeps the legacy AuthConfig write for downstream reactnative/invocation compat.
func PersistActivateCreds(logger log.Logger, isCI bool, cred configcommon.CacheAuthConfig, mpCfg *multiplatformconfig.Config) {
	if cred.IsJWT {
		mpCfg.AuthConfig = cred

		return
	}
	persistActivateCredsTo(logger, SelectAuto(isCI), cred, mpCfg)
}

// persistActivateCredsTo takes the store so tests can refuse a write without a
// real keychain.
func persistActivateCredsTo(logger log.Logger, target Store, cred configcommon.CacheAuthConfig, mpCfg *multiplatformconfig.Config) {
	if target.Backend() == auth.BackendFile {
		persistToFile(cred, mpCfg)
		logger.Infof("Saved auth credentials to the multiplatform config file (CI-safe — fastlane setup_ci swaps the keychain)")

		return
	}
	if err := target.Save(mergeActivateCreds(target, cred)); err != nil {
		// AuthConfig has no room for the refresh token; the config file does.
		logger.Warnf("Keychain save failed (%v); saving to the multiplatform config file instead", err)
		persistToFile(cred, mpCfg)

		return
	}
	logger.Infof("Saved auth credentials to the OS keychain")
}

// Merges against the file store, not the caller's: merging against an unreadable
// keychain is what yields a bare token.
func persistToFile(cred configcommon.CacheAuthConfig, mpCfg *multiplatformconfig.Config) {
	c := mergeActivateCreds(NewFile(), cred)
	mpCfg.Credentials = &c
	mpCfg.AuthConfig = cred
}

// The tokens are deliberately not compared: a login's PAT is short-lived and gets
// refreshed, so a mismatch is normal, and treating it as a different credential
// discarded the refresh token.
func mergeActivateCreds(target Store, cred configcommon.CacheAuthConfig) auth.TokenSet {
	existing, err := target.Load()
	if err != nil {
		return auth.TokenSet{AuthToken: cred.AuthToken, WorkspaceID: cred.WorkspaceID}
	}

	existing.AuthToken = cred.AuthToken
	existing.WorkspaceID = cred.WorkspaceID

	return existing
}

// SetUsername writes name into the store that already holds credentials so a
// username-only edit can't strand an empty-token entry in the wrong backend.
// Empty name clears the override. Returns the store written to.
func SetUsername(isCI bool, name string) (auth.Origin, error) {
	target, existing := storeHoldingCreds(isCI)
	existing.Username = strings.TrimSpace(name)
	if err := target.Save(existing); err != nil {
		return existing.Origin(target.Backend()), fmt.Errorf("save display name to %s: %w", target.Backend().String(), err)
	}

	return existing.Origin(target.Backend()), nil
}

func storeHoldingCreds(isCI bool) (Store, auth.TokenSet) {
	for _, s := range []Store{NewKeychain(), NewFile()} {
		creds, err := s.Load()
		if err == nil && (strings.TrimSpace(creds.AuthToken) != "" || strings.TrimSpace(creds.WorkspaceID) != "") {
			return s, creds
		}
	}

	target := SelectAuto(isCI)
	creds, _ := target.Load()

	return target, creds
}
