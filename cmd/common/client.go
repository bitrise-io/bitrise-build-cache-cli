package common

import (
	"context"
	"fmt"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/kv_storage"
)

const (
	ClientNameXcode             = "xcode"
	ClientNameGradleConfigCache = "gradle-config"
	ClientNameGradle            = "gradle"
)

type CreateKVClientParams struct {
	CacheOperationID   string
	ClientName         string
	InvocationID       string
	AuthConfig         common.CacheAuthConfig
	Envs               map[string]string
	CommandFunc        common.CommandFunc
	Logger             log.Logger
	BitriseKVClient    kv_storage.KVStorageClient         // nullable, if not provided, a new client will be created
	CapabilitiesClient remoteexecution.CapabilitiesClient // nullable, if not provided, a new client will be created
	SkipCapabilities   bool                               // if true, GetCapabilities will not be called
}

func CreateKVClient(ctx context.Context, params CreateKVClientParams) (*kv.Client, error) {
	endpointURL := common.SelectCacheEndpointURL("", params.Envs)
	params.Logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)

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
		CacheConfigMetadata: common.NewMetadata(params.Envs, params.CommandFunc, params.Logger),
		CacheOperationID:    params.CacheOperationID,
		BitriseKVClient:     params.BitriseKVClient,
		CapabilitiesClient:  params.CapabilitiesClient,
		InvocationID:        params.InvocationID,
	})
	if err != nil {
		return nil, fmt.Errorf("new kv client: %w", err)
	}

	if params.SkipCapabilities {
		return kvClient, nil
	}

	if err := kvClient.GetCapabilitiesWithRetry(ctx); err != nil {
		return nil, fmt.Errorf("get capabilities: %w", err)
	}

	return kvClient, nil
}
