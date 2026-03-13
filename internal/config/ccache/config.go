package ccache

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	ccachePath       = ".bitrise/cache/ccache/"
	ccacheConfigFile = "config.json"

	defaultLogFile            = "ccache-%s.log"
	defaultErrLogFile         = "ccache-err.log"
	defaultIdleTimeout        = "15m"
	defaultLayout             = "flat"
	defaultCRSHDataTimeout    = "2s"
	defaultCRSHRequestTimeout = "20s"

	ErrFmtOpenConfigFile   = "open ccache config file (%s): %w"
	ErrFmtDecodeConfigFile = "decode ccache config file (%s): %w"
	ErrFmtCreateConfigFile = "failed to create ccache config file: %w"
	ErrFmtEncodeConfigFile = "failed to encode ccache config file: %w"
	ErrFmtCreateFolder     = "failed to create .bitrise/cache/ccache folder (%s): %w"
	ErrNoAuthConfig        = "read auth config: %w"
)

// Params holds the parameters for creating a ccache activate config.
type Params struct {
	BuildCacheEndpoint    string
	PushEnabled           bool
	IPCSocketPathOverride string
	BaseDirOverride       string
}

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

func DefaultParams() Params {
	return Params{
		PushEnabled: true,
	}
}

func NewConfig(envs map[string]string, osProxy utils.OsProxy, params Params) (Config, error) {
	authConfig, err := common.ReadAuthConfigFromEnvironments(envs)
	if err != nil {
		return Config{}, fmt.Errorf(ErrNoAuthConfig, err)
	}

	ipcEndpoint := params.IPCSocketPathOverride
	if ipcEndpoint == "" {
		wd, err := osProxy.Getwd()
		if err != nil {
			wd = "."
		}
		ipcEndpoint = filepath.Join(wd, "ccache-ipc.sock")
	}

	buildCacheEndpoint := common.SelectCacheEndpointURL(params.BuildCacheEndpoint, envs)
	idleTimeout, _ := time.ParseDuration(defaultIdleTimeout)

	return Config{
		AuthConfig:         authConfig,
		IPCEndpoint:        ipcEndpoint,
		LogFile:            defaultLogFile,
		ErrLogFile:         defaultErrLogFile,
		IdleTimeout:        idleTimeout,
		Layout:             defaultLayout,
		PushEnabled:        params.PushEnabled,
		Enabled:            true,
		BuildCacheEndpoint: buildCacheEndpoint,
	}, nil
}

func (config Config) CRSHRemoteStorageURL() string {
	return fmt.Sprintf("crsh:%s data-timeout=%s request-timeout=%s",
		config.IPCEndpoint, defaultCRSHDataTimeout, defaultCRSHRequestTimeout)
}

func (config Config) Save(logger log.Logger, osProxy utils.OsProxy, encoderFactory utils.EncoderFactory) error {
	ccacheDir := DirPath(osProxy)
	if err := osProxy.MkdirAll(ccacheDir, 0o755); err != nil {
		return fmt.Errorf(ErrFmtCreateFolder, ccacheDir, err)
	}

	configFilePath := PathFor(osProxy, ccacheConfigFile)
	f, err := osProxy.Create(configFilePath)
	if err != nil {
		return fmt.Errorf(ErrFmtCreateConfigFile, err)
	}
	defer f.Close()

	enc := encoderFactory.Encoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(config); err != nil {
		return fmt.Errorf(ErrFmtEncodeConfigFile, err)
	}

	logger.TInfof("Config saved to: %s", configFilePath)

	return nil
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
