package common

import (
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcode"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	XceleratePath           = ".bitrise-xcelerate"
	XcelerateConfigFileName = "config.json"

	ErrFmtDetermineHome    = `could not determine home: %w`
	ErrFmtCreateConfigFile = `failed to create xcelerate config file: %w`
	ErrFmtEncodeConfigFile = `failed to encode xcelerate config file: %w`
	ErrFmtCreateFolder     = `failed to create .xcelerate folder (%s): %w`
)

type Xcelerate struct {
	Xcode xcode.Xcode `json:"xcode"`
}

func (config *Xcelerate) CreateConfig(os utils.OsProxy, encoder utils.EncoderProxyCreator) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf(ErrFmtDetermineHome, err)
	}

	xcelerateFolder := filepath.Join(home, XceleratePath)
	if err := os.MkdirAll(xcelerateFolder, 0755); err != nil {
		return fmt.Errorf(ErrFmtCreateFolder, xcelerateFolder, err)
	}

	configFilePath := filepath.Join(home, XceleratePath, XcelerateConfigFileName)
	f, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf(ErrFmtCreateConfigFile, err)
	}
	defer f.Close()

	enc := encoder(f)
	if err := enc.Encode(config); err != nil {
		return fmt.Errorf(ErrFmtEncodeConfigFile, err)
	}

	return nil
}
