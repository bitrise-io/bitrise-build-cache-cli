package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	ClientNameXcode             = "xcode"
	ClientNameGradleConfigCache = "gradle-config"
)

type CreateKVClientParams struct {
	CacheOperationID string
	ClientName       string
	AuthConfig       common.CacheAuthConfig
	EnvProvider      common.EnvProviderFunc
	Logger           log.Logger
}

func createKVClient(ctx context.Context,
	params CreateKVClientParams) (*kv.Client, error) {
	endpointURL := common.SelectCacheEndpointURL("", params.EnvProvider)
	params.Logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)

	if params.ClientName == ClientNameXcode &&
		(endpointURL == consts.EndpointURLATL1 || endpointURL == consts.EndpointURLLAS1) {
		return nil, fmt.Errorf("the selected endpoint %s is not supported", endpointURL)
	}

	buildCacheHost, insecureGRPC, err := kv.ParseURLGRPC(endpointURL)
	if err != nil {
		return nil, fmt.Errorf(
			"the url grpc[s]://host:port format, %q is invalid: %w",
			endpointURL, err,
		)
	}
	params.Logger.Debugf("Build Cache host: %s", buildCacheHost)

	kvClient, err := kv.NewClient(kv.NewClientParams{
		UseInsecure:         insecureGRPC,
		Host:                buildCacheHost,
		DialTimeout:         5 * time.Second,
		ClientName:          params.ClientName,
		AuthConfig:          params.AuthConfig,
		Logger:              params.Logger,
		CacheConfigMetadata: common.NewCacheConfigMetadata(params.EnvProvider),
		CacheOperationID:    params.CacheOperationID,
	})
	if err != nil {
		return nil, fmt.Errorf("new kv client: %w", err)
	}

	if err := kvClient.GetCapabilities(ctx); err != nil {
		return nil, fmt.Errorf("get capabilities: %w", err)
	}

	return kvClient, nil
}
