package bazelconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/clibin"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

var errLiteNeedsCredentialHelper = errors.New(
	"lite activation needs the CLI on $PATH or at a stable path so Bazel can call it as a credential helper; " +
		"install it (e.g. /usr/local/bin) before running activate --lite")

type CacheParams struct {
	Enabled     bool
	PushEnabled bool
	Endpoint    string
}

type BESParams struct {
	Enabled  bool
	Endpoint string
}

type RBEParams struct {
	Enabled  bool
	Endpoint string
}

type ActivateBazelParams struct {
	Cache      CacheParams
	BES        BESParams
	RBE        RBEParams
	Timestamps bool
	// CLIPath is how the generated bazelrc names the CLI in
	// `--credential_helper=<CLIPath>`, instead of embedding the auth token. An
	// absolute path normally; empty means fall back to the bare binary name,
	// which Bazel looks up in $PATH.
	CLIPath string

	// Lite writes the static wiring only: the credential helper line and the
	// endpoints. Auth and per-invocation metadata come back from `get` per build.
	Lite bool
}

func DefaultActivateBazelParams() ActivateBazelParams {
	return ActivateBazelParams{
		Cache: CacheParams{
			Enabled:     true,
			PushEnabled: true,
		},
		BES: BESParams{
			Enabled: true,
		},
		RBE: RBEParams{
			Enabled: false,
		},
		Timestamps: true,
	}
}

func (params ActivateBazelParams) TemplateInventory(
	ctx context.Context,
	logger log.Logger,
	envs map[string]string,
	commandFunc common.CommandFunc,
	isDebug bool,
) (TemplateInventory, error) {
	logger.Infof("(i) Checking parameters")

	commonInventory, err := params.commonTemplateInventory(ctx, logger, envs, commandFunc, isDebug)
	if err != nil {
		return TemplateInventory{}, err
	}

	cacheInventory := params.cacheTemplateInventory(logger, envs)
	besInventory := params.besTemplateInventory(logger)
	rbeInventory := params.rbeTemplateInventory(logger, envs)

	return TemplateInventory{
		Common: commonInventory,
		Cache:  cacheInventory,
		BES:    besInventory,
		RBE:    rbeInventory,
	}, nil
}

func (params ActivateBazelParams) commonTemplateInventory(
	ctx context.Context,
	logger log.Logger,
	envs map[string]string,
	commandFunc common.CommandFunc,
	isDebug bool,
) (CommonTemplateInventory, error) {
	logger.Infof("(i) Debug mode and verbose logs: %t", isDebug)

	// Required configs
	logger.Infof("(i) Check Auth Config")
	resolver := live.Default(nil)

	authConfig, _, _, err := resolver.ResolveAllowingNone(ctx, envs, params.Lite)
	if err != nil {
		return CommonTemplateInventory{},
			fmt.Errorf("resolve auth config: %w", err)
	}

	helperPath := credentialHelperPath(params.CLIPath)

	// Without a helper the bazelrc can only carry a literal token, and lite has
	// none to carry. Writing it anyway produces a config that can never
	// authenticate and bakes this machine's warmup metadata into every build, so
	// say so here rather than let it surface as a build-time auth failure.
	if params.Lite && helperPath == "" && (params.Cache.Enabled || params.BES.Enabled) {
		return CommonTemplateInventory{}, errLiteNeedsCredentialHelper
	}

	// ResolveUsername can reach the OS keychain, which has been seen to hang on a
	// macOS CI agent. Warmup emits no build user anyway, so do not ask.
	var username string
	if !params.Lite {
		username, _ = resolver.ResolveUsername(envs)
	}

	cacheConfig := common.NewMetadata(envs, username,
		commandFunc,
		utils.DefaultOsProxy{},
		logger)
	logger.Infof("(i) Cache Config: %+v", cacheConfig)

	// Structural, not environmental: warmup must emit no credential and no build
	// identity even when it happens to run inside a build that has both. The
	// helper resolves all of this per invocation. Host metadata stays — it
	// describes the machine, which is the one thing warmup does know.
	if params.Lite {
		authConfig = authpkg.Credential{}
		cacheConfig.BitriseAppID = ""
		cacheConfig.CIProvider = ""
		cacheConfig.GitMetadata.RepoURL = ""
		cacheConfig.BitriseWorkflowName = ""
		cacheConfig.BitriseBuildID = ""
		cacheConfig.HostMetadata.Username = ""
	}

	return CommonTemplateInventory{
		AuthToken:    authConfig.Token,
		WorkspaceID:  authConfig.WorkspaceID,
		Debug:        isDebug,
		AppSlug:      cacheConfig.BitriseAppID,
		CIProvider:   cacheConfig.CIProvider,
		RepoURL:      cacheConfig.GitMetadata.RepoURL,
		WorkflowName: cacheConfig.BitriseWorkflowName,
		BuildID:      cacheConfig.BitriseBuildID,
		Timestamps:   params.Timestamps,
		CLIPath:      helperPath,
		Lite:         params.Lite,
		HostMetadata: HostMetadataInventory{
			OS:             cacheConfig.HostMetadata.OS,
			Locale:         cacheConfig.HostMetadata.Locale,
			DefaultCharset: cacheConfig.HostMetadata.DefaultCharset,
			CPUCores:       cacheConfig.HostMetadata.CPUCores,
			MemSize:        cacheConfig.HostMetadata.MemSize,
			Username:       cacheConfig.HostMetadata.Username,
		},
	}, nil
}

func (params ActivateBazelParams) cacheTemplateInventory(
	logger log.Logger,
	envs map[string]string,
) CacheTemplateInventory {
	if !params.Cache.Enabled {
		logger.Infof("(i) Cache disabled")

		return CacheTemplateInventory{
			Enabled: false,
		}
	}

	logger.Infof("(i) Cache enabled")

	cacheEndpointURL := common.SelectCacheEndpointURL(params.Cache.Endpoint, envs)
	logger.Infof("(i) Build Cache Endpoint URL: %s", cacheEndpointURL)
	logger.Infof("(i) Push new cache entries: %t", params.Cache.PushEnabled)

	return CacheTemplateInventory{
		Enabled:             true,
		EndpointURLWithPort: cacheEndpointURL,
		IsPushEnabled:       params.Cache.PushEnabled,
	}
}

func (params ActivateBazelParams) besTemplateInventory(
	logger log.Logger,
) BESTemplateInventory {
	if !params.BES.Enabled {
		logger.Infof("(i) BES disabled")

		return BESTemplateInventory{
			Enabled: false,
		}
	}

	logger.Infof("(i) BES enabled")

	besEndpoint := params.BES.Endpoint
	if besEndpoint == "" {
		besEndpoint = "grpcs://flare-bes.services.bitrise.io:443"
	}
	logger.Infof("(i) Build Event Service Endpoint URL: %s", besEndpoint)

	return BESTemplateInventory{
		Enabled:             true,
		EndpointURLWithPort: besEndpoint,
	}
}

func (params ActivateBazelParams) rbeTemplateInventory(
	logger log.Logger,
	envs map[string]string,
) RBETemplateInventory {
	if !params.RBE.Enabled {
		logger.Infof("(i) RBE disabled")

		return RBETemplateInventory{
			Enabled: false,
		}
	}

	logger.Infof("(i) RBE enabled")

	rbeEndpoint := common.SelectRBEEndpointURL(params.RBE.Endpoint, envs)
	// If no endpoint is available, RBE should not be enabled
	if rbeEndpoint == "" {
		logger.Infof("(i) RBE is not available at this location")

		return RBETemplateInventory{
			Enabled: false,
		}
	}
	logger.Infof("(i) Remote Build Execution Endpoint URL: %s", rbeEndpoint)

	return RBETemplateInventory{
		Enabled:             true,
		EndpointURLWithPort: rbeEndpoint,
	}
}

// credentialHelperPath keeps the helper reachable when the caller has no usable
// absolute path: Bazel resolves a bare name through $PATH, which also survives a
// CLI upgrade that moves the binary.
//
// It stays empty when that bare name resolves to nothing, because the template
// reads an empty CLIPath as "embed the token instead" — a config with a token on
// disk still authenticates, whereas a helper that can never be spawned fails
// every build.
//
// The bare name is resolved against the PATH of whatever spawns Bazel, which is
// not necessarily the PATH activation saw. A build in another filesystem
// namespace — bazel inside a container, most commonly — resolves neither the
// bare name nor an absolute path unless the binary is mounted in, and warmup
// cannot know it will happen. Unguarded by design; the failure is loud (Bazel
// aborts with "Could not find file with name 'bitrise-build-cache' on PATH").
func credentialHelperPath(cliPath string) string {
	switch {
	case cliPath != "":
		return cliPath
	case clibin.OnPATH():
		return paths.CLIBinaryName
	}

	return ""
}
