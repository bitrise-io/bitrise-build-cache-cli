package ccache

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	ccachePath       = ".bitrise/cache/ccache/"
	ccacheConfigFile = "config.json"

	ErrFmtOpenConfigFile   = "open ccache config file (%s): %w"
	ErrFmtDecodeConfigFile = "decode ccache config file (%s): %w"
)

type Config struct {
	LogFile            string                 `json:"logFile,omitempty"`
	ErrLogFile         string                 `json:"errLogFile,omitempty"`
	IPCEndpoint        string                 `json:"ipcEndpoint,omitempty"`
	IdleTimeout        time.Duration          `json:"idleTimeout,omitempty"`
	Layout             string                 `json:"layout,omitempty"`
	PushEnabled        bool                   `json:"pushEnabled"`
	Enabled            bool                   `json:"enabled"`
	BuildCacheEndpoint string                 `json:"buildCacheEndpoint,omitempty"`
	AuthConfig         common.CacheAuthConfig `json:"authConfig,omitempty"`
}

func DirPath(osProxy utils.OsProxy) string {
	if home, err := osProxy.UserHomeDir(); err == nil {
		return filepath.Join(home, ccachePath)
	}

	if wd, err := osProxy.Getwd(); err == nil {
		return filepath.Join(wd, ccachePath)
	}

	if exe, err := osProxy.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return filepath.Join(dir, ccachePath)
		}
	}

	return filepath.Join(".", ccachePath)
}

func PathFor(osProxy utils.OsProxy, subpath string) string {
	return filepath.Join(DirPath(osProxy), subpath)
}

func ReadConfig(osProxy utils.OsProxy, decoderFactory utils.DecoderFactory) (Config, error) {
	configFilePath := PathFor(osProxy, ccacheConfigFile)

	f, err := os.OpenFile(configFilePath, 0, 0)
	if err != nil {
		return Config{}, fmt.Errorf(ErrFmtOpenConfigFile, configFilePath, err)
	}
	defer f.Close()

	dec := decoderFactory.Decoder(f)
	var config Config
	if err := dec.Decode(&config); err != nil {
		return Config{}, fmt.Errorf(ErrFmtDecodeConfigFile, configFilePath, err)
	}

	return config, nil
}
