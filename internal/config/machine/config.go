// Package machine holds the machine-wide build-cache config file.
package machine

import (
	"encoding/json"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type Mode string

const (
	ModeAlways Mode = "always"
	ModeOptIn  Mode = "opt-in"
)

type Config struct {
	ProjectMode Mode  `json:"project_mode,omitempty"`
	CachePush   *bool `json:"cache_push,omitempty"`
}

const DefaultCachePush = true

// Read returns the machine-wide config. A missing file resolves to an empty
// Config and no error.
func Read(osProxy utils.OsProxy, p paths.Paths, logger log.Logger) (Config, error) {
	content, exists, err := osProxy.ReadFileIfExists(p.MachineConfigFile())
	if err != nil {
		return Config{}, fmt.Errorf("read machine config: %w", err)
	}
	if !exists {
		return Config{}, nil
	}

	var cfg Config
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return Config{}, fmt.Errorf("parse machine config %s: %w", p.MachineConfigFile(), err)
	}

	cfg.ProjectMode = normaliseMode(cfg.ProjectMode, logger)

	return cfg, nil
}

// Write atomically persists the config file, creating its parent dir on demand.
func Write(cfg Config, osProxy utils.OsProxy, p paths.Paths) error {
	dir := p.BitriseCacheRoot()
	if err := osProxy.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create machine config dir %s: %w", dir, err)
	}

	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode machine config: %w", err)
	}

	final := p.MachineConfigFile()
	tmp := p.MachineConfigTempFile()
	if err := osProxy.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write machine config temp file: %w", err)
	}
	if err := osProxy.Rename(tmp, final); err != nil {
		return fmt.Errorf("promote machine config: %w", err)
	}

	return nil
}

// ResolvedProjectMode returns the persisted mode, defaulting to ModeAlways when
// the config file is missing or the field is unset.
func ResolvedProjectMode(cfg Config) Mode {
	if cfg.ProjectMode == ModeAlways || cfg.ProjectMode == ModeOptIn {
		return cfg.ProjectMode
	}

	return ModeAlways
}

// ResolvedCachePush returns the persisted push flag, defaulting to
// DefaultCachePush when the config file is missing or the field is unset.
func ResolvedCachePush(cfg Config) bool {
	if cfg.CachePush != nil {
		return *cfg.CachePush
	}

	return DefaultCachePush
}

// ValidateProjectMode returns nil for the two known modes and a descriptive
// error otherwise. Empty is invalid — callers gate on that before calling.
func ValidateProjectMode(m string) error {
	switch Mode(m) {
	case ModeAlways, ModeOptIn:
		return nil
	}

	return fmt.Errorf("invalid project mode %q, expected 'always' or 'opt-in'", m)
}

func normaliseMode(m Mode, logger log.Logger) Mode {
	switch m {
	case "":
		return ""
	case ModeAlways, ModeOptIn:
		return m
	}
	if logger != nil {
		logger.Warnf("Unknown project_mode %q in machine config, treating as 'always'.", string(m))
	}

	return ModeAlways
}
