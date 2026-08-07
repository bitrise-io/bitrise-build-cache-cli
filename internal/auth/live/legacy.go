package live

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// readLegacyFileCredential reads the multiplatform config's authConfig key, the
// pre-`credentials` layout that older analytics readers still write. Last in the
// precedence order: it carries no refresh token and no expiry.
func readLegacyFileCredential() (auth.Credential, bool) {
	cfg, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil || !cfg.AuthConfig.Populated() {
		return auth.Credential{}, false
	}

	return cfg.AuthConfig.Credential(), true
}

// writeLegacyFileCredential mirrors a credential into the analytics config's
// authConfig key without touching anything else in the file.
func writeLegacyFileCredential(cred auth.Credential) error {
	err := multiplatformconfig.Update(
		utils.DefaultOsProxy{}, utils.DefaultEncoderFactory{}, utils.DefaultDecoderFactory{},
		func(cfg *multiplatformconfig.Config) {
			cfg.AuthConfig = multiplatformconfig.LegacyAuthConfig{
				AuthToken:   cred.Token,
				WorkspaceID: cred.WorkspaceID,
				IsJWT:       true,
			}
		},
	)
	if err != nil {
		return fmt.Errorf("mirror credential into the analytics config: %w", err)
	}

	return nil
}
