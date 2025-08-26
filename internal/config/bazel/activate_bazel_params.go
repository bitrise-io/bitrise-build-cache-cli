package bazelconfig

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/go-utils/v2/log"
)

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
}

func DefaultActivateBazelParams() ActivateBazelParams {
	return ActivateBazelParams{
		Cache: CacheParams{
			Enabled:     true,
			PushEnabled: false,
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
	logger log.Logger,
	envs map[string]string,
	commandFunc common.CommandFunc,
	isDebug bool,
) (TemplateInventory, error) {
	logger.Infof("(i) Checking parameters")

	commonInventory, err := params.commonTemplateInventory(logger, envs, commandFunc, isDebug)
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
	logger log.Logger,
	envs map[string]string,
	commandFunc common.CommandFunc,
	isDebug bool,
) (CommonTemplateInventory, error) {
	logger.Infof("(i) Debug mode and verbose logs: %t", isDebug)

	// Required configs
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envs)
	if err != nil {
		return CommonTemplateInventory{},
			fmt.Errorf("read auth config from environment variables: %w", err)
	}

	cacheConfig := common.NewMetadata(envs,
		commandFunc,
		logger)
	logger.Infof("(i) Cache Config: %+v", cacheConfig)

	return CommonTemplateInventory{
		AuthToken:    authConfig.AuthToken,
		WorkspaceID:  authConfig.WorkspaceID,
		Debug:        isDebug,
		AppSlug:      cacheConfig.BitriseAppID,
		CIProvider:   cacheConfig.CIProvider,
		RepoURL:      cacheConfig.GitMetadata.RepoURL,
		WorkflowName: cacheConfig.BitriseWorkflowName,
		BuildID:      cacheConfig.BitriseBuildID,
		Timestamps:   params.Timestamps,
		HostMetadata: HostMetadataInventory{
			OS:             cacheConfig.HostMetadata.OS,
			Locale:         cacheConfig.HostMetadata.Locale,
			DefaultCharset: cacheConfig.HostMetadata.DefaultCharset,
			CPUCores:       cacheConfig.HostMetadata.CPUCores,
			MemSize:        cacheConfig.HostMetadata.MemSize,
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
