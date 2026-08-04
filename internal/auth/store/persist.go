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
	persistActivateCredsTo(logger, SelectAuto(envs), auth, mpCfg)
}

// persistActivateCredsTo is PersistActivateCreds with the target store supplied,
// so the keychain-refused branch is testable without a real keychain.
func persistActivateCredsTo(logger log.Logger, target Store, auth configcommon.CacheAuthConfig, mpCfg *multiplatformconfig.Config) {
	if target.Kind() == KindFile {
		persistToFile(auth, mpCfg)
		logger.Infof("Saved auth credentials to the multiplatform config file (CI-safe — fastlane setup_ci swaps the keychain)")

		return
	}
	if err := target.Save(mergeActivateCreds(target, auth)); err != nil {
		// Writing only AuthConfig here would strand the credential in a shape that
		// has no refresh token, so the login degrades to a bare PAT and dies when it
		// expires. A host with no usable keychain still has the config file.
		logger.Warnf("Keychain save failed (%v); saving to the multiplatform config file instead", err)
		persistToFile(auth, mpCfg)

		return
	}
	logger.Infof("Saved auth credentials to the OS keychain")
}

// persistToFile writes the full credential to the config file's Credentials
// block, keeping AuthConfig in step for downstream readers that still use it.
//
// The merge is against the file store on purpose: merging against an unusable
// keychain reads nothing and yields a bare token, which is precisely how the
// refresh token gets lost.
func persistToFile(auth configcommon.CacheAuthConfig, mpCfg *multiplatformconfig.Config) {
	c := mergeActivateCreds(NewFile(), auth)
	mpCfg.Credentials = &c
	mpCfg.AuthConfig = auth
}

// mergeActivateCreds keeps the OAuth fields and username of an existing entry
// when activate re-persists the same token. Replacing the entry outright would
// drop the refresh token, which both breaks `auth logout` and leaves the login
// unable to refresh — degrading it to a bare, short-lived PAT.
//
// A different token means a different credential, so the OAuth fields are not
// carried over: they would not describe the token being stored.
func mergeActivateCreds(target Store, auth configcommon.CacheAuthConfig) keychain.Credentials {
	existing, err := target.Load()
	if err != nil || existing.AuthToken != auth.AuthToken {
		return keychain.Credentials{AuthToken: auth.AuthToken, WorkspaceID: auth.WorkspaceID}
	}

	existing.AuthToken = auth.AuthToken
	existing.WorkspaceID = auth.WorkspaceID

	return existing
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
