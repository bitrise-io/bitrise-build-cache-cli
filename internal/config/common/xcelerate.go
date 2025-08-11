package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcode"
)

const (
	XceleratePath           = ".bitrise-xcelerate"
	XcelerateConfigFileName = "config.json"

	errFmtDetermineHome    = `could not determine home: %w`
	errFmtCreateConfigFile = `failed to create xcelerate config file: %w`
	errFmtEncodeConfigFile = `failed to encode xcelerate config file: %w`
	errFmtCreateFolder     = `failed to create .xcelerate folder (%s): %w`
)

type Xcelerate struct {
	Xcode xcode.Xcode `json:"xcode"`
}

func (config *Xcelerate) CreateXcodeConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf(errFmtDetermineHome, err)
	}

	xcelerateFolder := filepath.Join(home, XceleratePath)
	if err := os.MkdirAll(xcelerateFolder, 0755); err != nil {
		return fmt.Errorf(errFmtCreateFolder, xcelerateFolder, err)
	}

	configFilePath := filepath.Join(home, XceleratePath, XcelerateConfigFileName)
	f, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf(errFmtCreateConfigFile, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(config); err != nil {
		return fmt.Errorf(errFmtEncodeConfigFile, err)
	}

	return nil
}
