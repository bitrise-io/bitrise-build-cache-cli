package xcelerate

import (
	"fmt"

	"os"

	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	DefaultXcodePath        = "/usr/bin/xcodebuild"
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

func ReadConfig(decoderFactory utils.DecoderFactory) (Config, error) {
	configFilePath := PathFor(xcelerateConfigFileName)

	f, err := os.OpenFile(configFilePath, 0, 0)
	if err != nil {
		return Config{}, fmt.Errorf("open xcelerate config file (%s): %w", configFilePath, err)
	}
	defer f.Close()

	dec := decoderFactory.Decoder(f)
	var config Config
	if err := dec.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode xcelerate config file (%s): %w", configFilePath, err)
	}

	return config, nil
}

func DefaultConfig() Config {
	return Config{}
}

func NewConfig(ctx context.Context, logger log.Logger, params Params, envProvider common.EnvProviderFunc, cmdFunc utils.CommandFunc) Config {
	originalXcodebuildPath, err := getOriginalXcodebuildPath(ctx, logger, cmdFunc)
	if err != nil {
		logger.Warnf("Failed to determine xcodebuild path: %s. Using default: %s", err, DefaultXcodePath)
		originalXcodebuildPath = DefaultXcodePath
	} else {
		logger.Infof("Using xcodebuild path: %s", originalXcodebuildPath)
	}

	return Config{
		ProxyVersion:           envProvider("BITRISE_XCELERATE_PROXY_VERSION"),
		WrapperVersion:         envProvider("BITRISE_XCELERATE_WRAPPER_VERSION"),
		CLIVersion:             envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
		OriginalXcodebuildPath: originalXcodebuildPath,
		BuildCacheEnabled:      params.BuildCacheEnabled,
		DebugLogging:           params.DebugLogging,
	}
}

func getOriginalXcodebuildPath(ctx context.Context, logger log.Logger, cmdFunc utils.CommandFunc) (string, error) {
	logger.Debugf("Determining original xcodebuild path...")
	cmd := cmdFunc(ctx, "which", "xcodebuild")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get xcodebuild output: %w", err)
	}
	if len(output) == 0 {
		logger.Warnf("No xcodebuild path found, using default: %s", DefaultXcodePath)

		return DefaultXcodePath, nil
	}

	return string(output), nil
}

func (config Config) Save(os utils.OsProxy, encoderFactory utils.EncoderFactory) error {
	xcelerateFolder := DirPath()

	if err := os.MkdirAll(xcelerateFolder, 0755); err != nil {
		return fmt.Errorf(ErrFmtCreateFolder, xcelerateFolder, err)
	}

	configFilePath := PathFor(xcelerateConfigFileName)
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
