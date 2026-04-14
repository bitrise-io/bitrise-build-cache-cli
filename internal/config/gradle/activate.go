package gradleconfig

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
)

const (
	ErrFmtFailedToUpdateProps = "failed to update gradle.properties: %w"
)

// Activate creates the Gradle init script and updates gradle.properties
// to enable Bitrise Build Cache.
func Activate(
	logger log.Logger,
	gradleHomePath string,
	envProvider map[string]string,
	debugLogging bool,
	templateInventoryProvider func(log.Logger, map[string]string, bool, configcommon.BenchmarkPhaseProvider) (TemplateInventory, error),
	templateWriter func(TemplateInventory, string) error,
	updater GradlePropertiesUpdater,
	params ActivateGradleParams,
) error {
	authConfig, _ := configcommon.ReadAuthConfigFromEnvironments(envProvider)
	benchmarkClient := configcommon.NewBenchmarkPhaseClient(consts.BitriseWebsiteBaseURL, authConfig, logger)

	templateInventory, err := templateInventoryProvider(logger, envProvider, debugLogging, benchmarkClient)
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
