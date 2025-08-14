package xcelerate

import (
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	xceleratePath           = ".bitrise-xcelerate"
	xcelerateConfigFileName = "config.json"

	ErrFmtDetermineHome    = `could not determine home: %w`
	ErrFmtCreateConfigFile = `failed to create xcelerate config file: %w`
	ErrFmtEncodeConfigFile = `failed to encode xcelerate config file: %w`
	ErrFmtCreateFolder     = `failed to create .xcelerate folder (%s): %w`
)

//go:generate moq -out mocks/config_mock.go -pkg mocks . Config
type Config interface {
	GetProxyVersion() string
	GetWrapperVersion() string
	GetOriginalXcodebuildPath() string
	GetBuildCacheEnabled() bool

	Save(os utils.OsProxy, encoderFactory utils.EncoderFactory) error
}

type DefaultConfig struct {
	ProxyVersion           string `json:"proxyVersion"`
	WrapperVersion         string `json:"wrapperVersion"`
	OriginalXcodebuildPath string `json:"originalXcodebuildPath"`
	BuildCacheEnabled      bool   `json:"buildCacheEnabled"`
}

func (config DefaultConfig) GetProxyVersion() string {
	return config.ProxyVersion
}

func (config DefaultConfig) GetWrapperVersion() string {
	return config.WrapperVersion
}

func (config DefaultConfig) GetOriginalXcodebuildPath() string {
	return config.OriginalXcodebuildPath
}

func (config DefaultConfig) GetBuildCacheEnabled() bool {
	return config.BuildCacheEnabled
}

func (config DefaultConfig) Save(os utils.OsProxy, encoderFactory utils.EncoderFactory) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf(ErrFmtDetermineHome, err)
	}

	xcelerateFolder := filepath.Join(home, xceleratePath)
	if err := os.MkdirAll(xcelerateFolder, 0755); err != nil {
		return fmt.Errorf(ErrFmtCreateFolder, xcelerateFolder, err)
	}

	configFilePath := filepath.Join(home, xceleratePath, xcelerateConfigFileName)
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
