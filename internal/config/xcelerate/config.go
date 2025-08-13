package xcelerate

import (
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	xceleratePath           = ".bitrise-xcelerate"
	xcelerateConfigFileName = "config.json"

	errFmtDetermineHome    = `could not determine home: %w`
	errFmtCreateConfigFile = `failed to create xcelerate config file: %w`
	errFmtEncodeConfigFile = `failed to encode xcelerate config file: %w`
	errFmtCreateFolder     = `failed to create .xcelerate folder (%s): %w`
)

type Config interface {
	GetProxyVersion() string
	GetWrapperVersion() string
	GetOriginalXcodebuildPath() string
	GetBuildCacheEnabled() bool

	Save(os utils.OsProxy, encoderFactory utils.EncoderFactory) error
}

type DefaultConfig struct {
	ProxyVersion           string `json:"proxy_version"`
	WrapperVersion         string `json:"wrapper_version"`
	OriginalXcodebuildPath string `json:"original_xcodebuild_path"`
	BuildCacheEnabled      bool   `json:"build_cache_enabled"`
}

func (config *DefaultConfig) GetProxyVersion() string {
	return config.ProxyVersion
}

func (config *DefaultConfig) GetWrapperVersion() string {
	return config.WrapperVersion
}

func (config *DefaultConfig) GetOriginalXcodebuildPath() string {
	return config.OriginalXcodebuildPath
}

func (config *DefaultConfig) GetBuildCacheEnabled() bool {
	return config.BuildCacheEnabled
}

func (config *DefaultConfig) Save(os utils.OsProxy, encoderFactory utils.EncoderFactory) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf(errFmtDetermineHome, err)
	}

	xcelerateFolder := filepath.Join(home, xceleratePath)
	if err := os.MkdirAll(xcelerateFolder, 0755); err != nil {
		return fmt.Errorf(errFmtCreateFolder, xcelerateFolder, err)
	}

	configFilePath := filepath.Join(home, xceleratePath, xcelerateConfigFileName)
	f, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf(errFmtCreateConfigFile, err)
	}
	defer f.Close()

	enc := encoderFactory.Encoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(config); err != nil {
		return fmt.Errorf(errFmtEncodeConfigFile, err)
	}

	return nil
}
