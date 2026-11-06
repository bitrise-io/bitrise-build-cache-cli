package proxy

import (
	"errors"
	"sync"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildrequest"
)

// buildRequestInfoCache resolves (action, qos) from the sidecar's own
// build-request.json and caches by (ddPath, hex). A central-scheduled flush
// often touches dozens of connections sharing the same build-request; a
// per-conn disk read each time would stall the tick.
type buildRequestInfoCache struct {
	logger log.Logger

	mu    sync.Mutex
	cache map[string]buildrequest.Info
}

// NewBuildRequestInfoLoader returns a cached loader that reads action/qos
// from <DD>/.../<hex>.xcbuilddata/build-request.json. Missing or malformed
// files cache the empty tuple so a repeat miss doesn't retry disk every tick.
func NewBuildRequestInfoLoader(logger log.Logger) func(ddPath, hex string) (string, string) {
	c := &buildRequestInfoCache{
		logger: logger,
		cache:  make(map[string]buildrequest.Info),
	}

	return c.load
}

func (c *buildRequestInfoCache) load(ddPath, hex string) (string, string) {
	if ddPath == "" || hex == "" {
		return "", ""
	}

	key := ddPath + "\x00" + hex

	c.mu.Lock()
	cached, ok := c.cache[key]
	c.mu.Unlock()

	if ok {
		return cached.Action, cached.QoS
	}

	info, err := buildrequest.Load(ddPath, hex)
	if err != nil {
		if c.logger != nil && !errors.Is(err, buildrequest.ErrNotFound) {
			c.logger.Debugf("proxy build-request load %s/%s failed: %s", ddPath, hex, err)
		}

		info = buildrequest.Info{}
	}

	c.mu.Lock()
	c.cache[key] = info
	c.mu.Unlock()

	return info.Action, info.QoS
}
