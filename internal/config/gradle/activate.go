package gradleconfig

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/envexport"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	ErrFmtFailedToUpdateProps = "failed to update gradle.properties: %w"
)

// Seam: tests point activation at a temporary credential store instead of the real one.
//
//nolint:gochecknoglobals
var newResolver = live.Default

// Activate creates the Gradle init script and updates gradle.properties
// to enable Bitrise Build Cache.
func Activate(
	ctx context.Context,
	logger log.Logger,
	gradleHomePath string,
	envProvider map[string]string,
	debugLogging bool,
	templateInventoryProvider func(context.Context, log.Logger, map[string]string, bool, configcommon.BenchmarkPhaseProvider, utils.OsProxy) (TemplateInventory, error),
	templateWriter func(TemplateInventory, string) error,
	updater GradlePropertiesUpdater,
	params ActivateGradleParams,
) error {
	NormalizeParams(&params)

	resolver := newResolver(logger)

	// Pinned: the plugins run `bitrise-build-cache auth token` mid-build, by which time the env
	// vars activation resolved from may be gone.
	authConfig, _, err := resolver.ResolvePinned(ctx, envProvider, configcommon.IsCI(envProvider, utils.DefaultOsProxy{}))
	if err != nil {
		return fmt.Errorf(ErrFmtReadAuthConfig, err)
	}

	benchmarkClient := configcommon.NewBenchmarkPhaseClient(consts.BitriseWebsiteBaseURL, authConfig, logger)

	username, _ := resolver.ResolveUsername(envProvider)
	metadata := configcommon.NewMetadata(envProvider, username,
		func(name string, v ...string) (string, error) {
			output, err := exec.Command(name, v...).Output() //nolint:noctx

			return string(output), err
		}, utils.DefaultOsProxy{}, logger)

	// Always export CLI path, CLI version, and the pinned plugin versions, independent of CI.
	// The init script reads BITRISE_BUILD_CACHE_GRADLE_*_PLUGIN_VERSION via System.getenv so that
	// the init-script body stays byte-stable across CLI releases — critical for Gradle's
	// configuration cache fingerprint across save/restore shards running different CLI versions.
	exporter := envexport.New(envProvider, logger)
	//nolint:contextcheck // envman export is fire-and-forget, EnvExporter takes no context
	exporter.ExportCLIPath()
	//nolint:contextcheck // envman export is fire-and-forget, EnvExporter takes no context
	exporter.ExportCLIVersion(configcommon.GetCLIVersion(logger))
	//nolint:contextcheck // envman export is fire-and-forget, EnvExporter takes no context
	exporter.ExportGradlePluginVersions(
		consts.GradleCommonPluginDepVersion,
		consts.GradleAnalyticsPluginDepVersion,
		consts.GradleRemoteBuildCachePluginDepVersion,
		consts.GradleTestDistributionPluginDepVersion,
	)

	if configcommon.IsCI(envProvider, utils.DefaultOsProxy{}) && metadata.CIProvider != "" {
		ApplyBenchmarkPhase(&params, logger, benchmarkClient, metadata, exporter)
	}

	templateInventory, err := templateInventoryProvider(ctx, logger, envProvider, debugLogging, benchmarkClient, utils.DefaultOsProxy{})
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
