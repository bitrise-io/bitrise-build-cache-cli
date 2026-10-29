package xcelerate

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"golang.org/x/mod/semver"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// compressionConfigVersion is the first config version that carries the zstd blob compression setting.
const compressionConfigVersion = "1.1.0"

// ConfigMigrator implements toolconfig.Migrator for ~/.bitrise-xcelerate/config.json.
type ConfigMigrator struct {
	Logger log.Logger
}

func (ConfigMigrator) Tool() toolconfig.Tool { return toolconfig.Xcelerate }

func (m ConfigMigrator) Migrate(_ string) error {
	osProxy := utils.DefaultOsProxy{}

	cfg, err := ReadConfig(osProxy, utils.DefaultDecoderFactory{}, utils.AllEnvs())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("read xcelerate config: %w", err)
	}

	// Configs written before 1.1.0 have no compression setting; compression is the default now, so switch it on once.
	// A config at 1.1.0 or later was written with an explicit choice and is left alone.
	if predates(cfg.ConfigVersion, compressionConfigVersion) {
		cfg.RemoteCacheCompression = true
		cfg.RemoteCacheCompressionThreshold = DefaultCompressionThreshold
	}

	cfg.ConfigVersion = toolconfig.XcelerateConfigVersion
	cfg.WrittenAt = time.Now().UTC()

	if err := cfg.Save(m.Logger, osProxy, utils.DefaultEncoderFactory{}); err != nil {
		return fmt.Errorf("rewrite xcelerate config: %w", err)
	}

	return nil
}

// predates reports whether stored is older than version; a missing or invalid stored version counts as 1.0.0.
func predates(stored, version string) bool {
	storedV := "v" + strings.TrimPrefix(stored, "v")
	if !semver.IsValid(storedV) {
		storedV = "v1.0.0"
	}

	return semver.Compare(storedV, "v"+version) < 0
}
