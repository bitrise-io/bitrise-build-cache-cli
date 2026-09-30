package bazelcredhelper

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// NewEntitlementSkipper answers the gate for a lite-activated machine. The
// answer is resolved once per build and cached, because Bazel spawns the helper
// once per RPC and an uncached check would be a request each time.
func NewEntitlementSkipper(envs map[string]string, logger log.Logger) EntitlementSkipper {
	return func(ctx context.Context, cred Credential) bool {
		metadata := configcommon.NewMetadata(envs, "", func(string, ...string) (string, error) {
			return "", nil
		}, utils.DefaultOsProxy{}, logger)

		return configcommon.SkipForEntitlementAtBuildTime(
			ctx,
			consts.BitriseWebsiteBaseURL,
			auth.Credential{Token: cred.Token, WorkspaceID: cred.WorkspaceID},
			metadata,
			logger,
		)
	}
}
