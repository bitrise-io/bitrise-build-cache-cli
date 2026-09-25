package live

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Cross-process read for the CI JWT that pin.go persists into the analytics
// block: TemplateInventory's ResolveNoRefresh has no broker, so a Build Hub
// runner that only holds a brokered JWT needs this read to find it back.
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
