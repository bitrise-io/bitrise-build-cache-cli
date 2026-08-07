package gradleconfig

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/envexport"
)

const (
	ErrFmtFailedToUpdateProps = "failed to update gradle.properties: %w"
)

// TemplateInventoryProvider builds a Gradle init-script inventory from
// pre-resolved metadata and the (post-benchmark) params. Activate owns the
// benchmark call and passes the mutated params here so callers do not
// re-fetch metadata or re-apply the phase.
type TemplateInventoryProvider func(
	logger log.Logger,
	envs map[string]string,
	isDebug bool,
	metadata configcommon.CacheConfigMetadata,
	params ActivateGradleParams,
) (TemplateInventory, error)

func Activate(
	logger log.Logger,
	gradleHomePath string,
	envProvider map[string]string,
	debugLogging bool,
	templateInventoryProvider TemplateInventoryProvider,
	templateWriter func(TemplateInventory, string) error,
	updater GradlePropertiesUpdater,
	params ActivateGradleParams,
) error {
	NormalizeParams(&params)

	authConfig, _, err := configcommon.ResolveAuthConfig(envProvider)
	if err != nil {
		return fmt.Errorf(ErrFmtReadAuthConfig, err)
	}

	metadata := configcommon.DefaultMetadata(envProvider, logger)

	if metadata.CIProvider != "" {
		benchmarkClient := configcommon.NewBenchmarkPhaseClient(consts.BitriseWebsiteBaseURL, authConfig, logger)
		ApplyBenchmarkPhase(&params, logger, benchmarkClient, metadata, envexport.New(envProvider, logger))
	}

	templateInventory, err := templateInventoryProvider(logger, envProvider, debugLogging, metadata, params)
	if err != nil {
		return err
	}

	if err := templateWriter(templateInventory, gradleHomePath); err != nil {
		return err
	}

	if err := updater.UpdateGradleProps(params, logger, gradleHomePath); err != nil {
		return fmt.Errorf(ErrFmtFailedToUpdateProps, err)
	}

	return nil
}

// DefaultTemplateInventoryProvider adapts ActivateGradleParams.TemplateInventory
// to the TemplateInventoryProvider callback shape used by Activate.
func DefaultTemplateInventoryProvider(
	logger log.Logger,
	envs map[string]string,
	isDebug bool,
	metadata configcommon.CacheConfigMetadata,
	params ActivateGradleParams,
) (TemplateInventory, error) {
	return params.TemplateInventory(logger, envs, isDebug, metadata)
}
