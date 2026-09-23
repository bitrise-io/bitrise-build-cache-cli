package live

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Mirrors a credential into the analytics block without touching the rest of the
// file. A CI JWT goes only here: it is minted per build, so putting it in the
// credentials block would make a 30-minute token look like a durable login.
func writeAnalyticsCredential(cred auth.Credential, origin auth.Origin) error {
	err := multiplatformconfig.Update(
		utils.DefaultOsProxy{}, utils.DefaultEncoderFactory{}, utils.DefaultDecoderFactory{},
		func(cfg *multiplatformconfig.Config) {
			cfg.AuthConfig = multiplatformconfig.NewAnalyticsAuthConfig(cred, origin)
		},
	)
	if err != nil {
		return fmt.Errorf("mirror credential into the analytics config: %w", err)
	}

	return nil
}
