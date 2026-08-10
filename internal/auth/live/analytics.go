package live

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Last in the precedence order: the analytics block carries no refresh token and
// no expiry, so anything else on the machine is a better answer.
func readAnalyticsCredential() (auth.Credential, auth.Origin, bool) {
	cfg, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil || !cfg.AuthConfig.Populated() {
		return auth.Credential{}, auth.Origin{}, false
	}

	return cfg.AuthConfig.Credential(), cfg.AuthConfig.Origin(), true
}

// Mirrors a credential into the analytics block without touching the rest of the
// file. A CI JWT goes only here: it is minted per build, so putting it in the
// credentials block would make a 30-minute token look like a durable login.
func writeAnalyticsCredential(cred auth.Credential) error {
	err := multiplatformconfig.Update(
		utils.DefaultOsProxy{}, utils.DefaultEncoderFactory{}, utils.DefaultDecoderFactory{},
		func(cfg *multiplatformconfig.Config) {
			cfg.AuthConfig = multiplatformconfig.AnalyticsAuthConfig{
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
