package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/go-utils/retry"
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
	CommandFunc      common.CommandFunc
	Logger           log.Logger
}

func createKVClient(ctx context.Context,
	params CreateKVClientParams) (*kv.Client, error) {
	endpointURL := common.SelectCacheEndpointURL("", params.EnvProvider)
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
		CacheConfigMetadata: common.NewMetadata(params.EnvProvider, params.CommandFunc, params.Logger),
		CacheOperationID:    params.CacheOperationID,
	})
	if err != nil {
		return nil, fmt.Errorf("new kv client: %w", err)
	}

	err = retry.Times(10).Wait(3 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
		if attempt > 0 {
			params.Logger.Debugf("Retrying GetCapabilities... (attempt %d)", attempt)
		}

		if err := kvClient.GetCapabilities(ctx); err != nil {
			params.Logger.Errorf("Error in GetCapabilities attempt %d: %s", attempt, err)
			if errors.Is(err, kv.ErrCacheUnauthenticated) {
				return kv.ErrCacheUnauthenticated, true
			}

			return fmt.Errorf("get capabilities: %w", err), false
		}

		return nil, false
	})
	if err != nil {
		return nil, fmt.Errorf("with retries: %w", err)
	}

	return kvClient, nil
}
