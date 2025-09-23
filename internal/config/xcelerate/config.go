package xcelerate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	DefaultXcodePath        = "/usr/bin/xcodebuild"
	xcelerateConfigFileName = "config.json"

	ErrFmtCreateConfigFile = `failed to create xcelerate config file: %w`
	ErrFmtEncodeConfigFile = `failed to encode xcelerate config file: %w`
	ErrFmtCreateFolder     = `failed to create .xcelerate folder (%s): %w`
	ErrNoAuthConfig        = "read auth config: %w"
)

type Params struct {
	BuildCacheEnabled       bool
	BuildCacheEndpoint      string
	DebugLogging            bool
	Silent                  bool
	XcodePathOverride       string
	ProxySocketPathOverride string
	PushEnabled             bool
}

type Config struct {
	ProxyVersion           string                 `json:"proxyVersion"`
	ProxySocketPath        string                 `json:"proxySocketPath"`
	CLIVersion             string                 `json:"cliVersion"`
	WrapperVersion         string                 `json:"wrapperVersion"`
	OriginalXcodebuildPath string                 `json:"originalXcodebuildPath"`
	BuildCacheEnabled      bool                   `json:"buildCacheEnabled"`
	BuildCacheEndpoint     string                 `json:"buildCacheEndpoint"`
	PushEnabled            bool                   `json:"pushEnabled"`
	DebugLogging           bool                   `json:"debugLogging,omitempty"`
	Silent                 bool                   `json:"silent,omitempty"`
	AuthConfig             common.CacheAuthConfig `json:"authConfig,omitempty"`
}

func ReadConfig(osProxy utils.OsProxy, decoderFactory utils.DecoderFactory) (Config, error) {
	configFilePath := PathFor(osProxy, xcelerateConfigFileName)

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

func DefaultParams() Params {
	return Params{
		BuildCacheEnabled:       true,
		BuildCacheEndpoint:      "",
		Silent:                  false,
		DebugLogging:            false,
		XcodePathOverride:       "",
		ProxySocketPathOverride: "",
		PushEnabled:             true,
	}
}

func DefaultConfig() Config {
	return Config{}
}

func NewConfig(ctx context.Context,
	logger log.Logger,
	params Params,
	envs map[string]string,
	osProxy utils.OsProxy,
	cmdFunc utils.CommandFunc,
) (Config, error) {
	authConfig, err := common.ReadAuthConfigFromEnvironments(envs)
	if err != nil {
		return Config{}, fmt.Errorf(ErrNoAuthConfig, err)
	}

	xcodePath := params.XcodePathOverride
	if xcodePath == "" {
		logger.Debugf("No xcodebuild path override specified, determining original xcodebuild path...")
		originalXcodebuildPath, err := getOriginalXcodebuildPath(ctx, logger, cmdFunc)
		if err != nil {
			logger.Warnf("Failed to determine xcodebuild path: %s. Using default: %s", err, DefaultXcodePath)
			originalXcodebuildPath = DefaultXcodePath
		}
		xcodePath = originalXcodebuildPath
	}
	logger.Infof("Using xcodebuild path: %s. You can always override this by supplying --xcode-path.", xcodePath)

	proxySocketPath := params.ProxySocketPathOverride
	if proxySocketPath == "" {
		proxySocketPath = envs["BITRISE_XCELERATE_PROXY_SOCKET_PATH"]
		if proxySocketPath == "" {
			proxySocketPath = filepath.Join(osProxy.TempDir(), "xcelerate-proxy.sock")
			logger.Infof("Using new proxy socket path: %s", proxySocketPath)
		} else {
			logger.Infof("Using proxy socket path from environment: %s", proxySocketPath)
		}
	}

	if params.BuildCacheEndpoint == "" {
		params.BuildCacheEndpoint = common.SelectCacheEndpointURL("", envs)
	}
	logger.Infof("Using Build Cache Endpoint: %s. You can always override this by supplying --cache-endpoint.", params.BuildCacheEndpoint)

	if params.DebugLogging && params.Silent {
		logger.Warnf("Both debug and silent logging specified, silent will take precedence.")
		params.DebugLogging = false
	}

	return Config{
		ProxyVersion:           envs["BITRISE_XCELERATE_PROXY_VERSION"],
		ProxySocketPath:        proxySocketPath,
		WrapperVersion:         envs["BITRISE_XCELERATE_WRAPPER_VERSION"],
		CLIVersion:             envs["BITRISE_BUILD_CACHE_CLI_VERSION"],
		OriginalXcodebuildPath: xcodePath,
		BuildCacheEnabled:      params.BuildCacheEnabled,
		BuildCacheEndpoint:     params.BuildCacheEndpoint,
		PushEnabled:            params.PushEnabled,
		DebugLogging:           params.DebugLogging,
		Silent:                 params.Silent,
		AuthConfig:             authConfig,
	}, nil
}

func getOriginalXcodebuildPath(ctx context.Context, logger log.Logger, cmdFunc utils.CommandFunc) (string, error) {
	logger.Debugf("Determining original xcodebuild path...")
	cmd := cmdFunc(ctx, "which", "xcodebuild")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get xcodebuild output: %w", err)
	}
	trimmed := strings.TrimSpace(string(output))
	if len(trimmed) == 0 {
		logger.Warnf("No xcodebuild path found, using default: %s", DefaultXcodePath)

		return DefaultXcodePath, nil
	}

	return trimmed, nil
}

func (config Config) Save(logger log.Logger, os utils.OsProxy, encoderFactory utils.EncoderFactory) error {
	xcelerateFolder := DirPath(os)

	if err := os.MkdirAll(xcelerateFolder, 0o755); err != nil {
		return fmt.Errorf(ErrFmtCreateFolder, xcelerateFolder, err)
	}

	configFilePath := PathFor(os, xcelerateConfigFileName)
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

	logger.TInfof("Config saved to: %s", configFilePath)

	return nil
}
