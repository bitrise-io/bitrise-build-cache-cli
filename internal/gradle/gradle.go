package gradle

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/go-utils/v2/log"
)

type Cache struct {
	logger      log.Logger
	kvClient    *kv.Client
	envProvider func(string) string
}

func NewCache(logger log.Logger, envProvider func(string) string, client *kv.Client) *Cache {
	return &Cache{
		logger:      logger,
		envProvider: envProvider,
		kvClient:    client,
	}
}
