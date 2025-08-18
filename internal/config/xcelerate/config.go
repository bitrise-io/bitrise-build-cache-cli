package xcelerate

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	xcelerateConfigFileName = "config.json"

	ErrFmtCreateConfigFile = `failed to create xcelerate config file: %w`
	ErrFmtEncodeConfigFile = `failed to encode xcelerate config file: %w`
	ErrFmtCreateFolder     = `failed to create .xcelerate folder (%s): %w`
)

type Params struct {
	BuildCacheEnabled bool
	DebugLogging      bool
}

type Config struct {
	ProxyVersion           string `json:"proxyVersion"`
	CLIVersion             string `json:"cliVersion"`
	WrapperVersion         string `json:"wrapperVersion"`
	OriginalXcodebuildPath string `json:"originalXcodebuildPath"`
	BuildCacheEnabled      bool   `json:"buildCacheEnabled"`
	DebugLogging           bool   `json:"debugLogging,omitempty"`
}

func NewConfig(params Params, envProvider common.EnvProviderFunc) Config {
	return Config{
		ProxyVersion:           envProvider("BITRISE_XCELERATE_PROXY_VERSION"),
		WrapperVersion:         envProvider("BITRISE_XCELERATE_WRAPPER_VERSION"),
		CLIVersion:             envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
		OriginalXcodebuildPath: "/usr/bin/xcodebuild",
		BuildCacheEnabled:      params.BuildCacheEnabled,
		DebugLogging:           params.DebugLogging,
	}
}

func (config Config) Save(os utils.OsProxy, encoderFactory utils.EncoderFactory) error {
	xcelerateFolder := XcelerateDirPath()

	if err := os.MkdirAll(xcelerateFolder, 0755); err != nil {
		return fmt.Errorf(ErrFmtCreateFolder, xcelerateFolder, err)
	}

	configFilePath := XceleratePathFor(xcelerateConfigFileName)
	f, err := os.Create(configFilePath)
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

	return nil
}
