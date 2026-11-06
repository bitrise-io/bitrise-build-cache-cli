package proxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// from <DD>/.../<hex>.xcbuilddata/build-request.json. Positive results are
// cached; ErrNotFound is not — Xcode may write build-request.json a few ms
// after the first sidecar flush, and a poisoned cache would strand that hex
// as unclassified for the rest of the build.
func NewBuildRequestInfoLoader(logger log.Logger) func(ddPath, hex string) (string, string) {
	c := &buildRequestInfoCache{
		logger: logger,
		cache:  make(map[string]buildrequest.Info),
	}

	return c.load
}

func (c *buildRequestInfoCache) load(ddPath, hex string) (string, string) {
	if hex == "" {
		return "", ""
	}

	// IDE sidecars (SWBBuildService path) ship with DerivedDataPath == "" in
	// build-info.json. Enumerate every DD root under $HOME and probe for the
	// hex; the first hit wins. Cache under a blank-ddPath key so every empty-DD
	// sidecar from the same build shares one lookup.
	key := ddPath + "\x00" + hex

	c.mu.Lock()
	cached, ok := c.cache[key]
	c.mu.Unlock()

	if ok {
		return cached.Action, cached.QoS
	}

	info, err := c.resolve(ddPath, hex)
	if err != nil {
		if errors.Is(err, buildrequest.ErrNotFound) {
			return "", ""
		}

		if c.logger != nil {
			c.logger.Debugf("proxy build-request load %s/%s failed: %s", ddPath, hex, err)
		}

		info = buildrequest.Info{}
	}

	c.mu.Lock()
	c.cache[key] = info
	c.mu.Unlock()

	return info.Action, info.QoS
}

func (c *buildRequestInfoCache) resolve(ddPath, hex string) (buildrequest.Info, error) {
	if ddPath != "" {
		info, err := buildrequest.Load(ddPath, hex)
		if err != nil {
			return info, fmt.Errorf("build-request load: %w", err)
		}

		return info, nil
	}

	roots := allDerivedDataRoots()
	if len(roots) == 0 {
		return buildrequest.Info{}, buildrequest.ErrNotFound
	}

	for _, root := range roots {
		info, err := buildrequest.Load(root, hex)
		if err == nil {
			return info, nil
		}

		if !errors.Is(err, buildrequest.ErrNotFound) {
			return buildrequest.Info{}, fmt.Errorf("build-request load: %w", err)
		}
	}

	return buildrequest.Info{}, buildrequest.ErrNotFound
}

// allDerivedDataRoots globs $HOME/Library/Developer/Xcode/DerivedData/* and
// returns every per-workspace DD root. The default shipping layout is the only
// one covered here — a user who moved DD via Settings > Locations is already
// taken care of by the non-empty ddPath code path (sidecar carries the custom
// root explicitly, which is the only case where build-info.json records it).
func allDerivedDataRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}

	matches, err := filepath.Glob(filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData", "*"))
	if err != nil {
		return nil
	}

	return matches
}
