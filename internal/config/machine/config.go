// Package machine holds the machine-wide build-cache config file.
package machine

import (
	"encoding/json"
	"errors"
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
	ProjectMode Mode `json:"project_mode,omitempty"`
}

// Read returns the machine-wide config. A missing file resolves to an empty
// Config and no error — the "not written yet" case is a valid state.
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

// Effective picks the mode a tool should honour given an explicit flag value
// (empty when unset) and the mode currently on disk. Empty flag + empty current
// falls back to ModeAlways so a machine without a stored preference keeps the
// prior behavior. An unknown flag returns an error — callers are expected to
// pre-validate with ValidateFlag, so this is a safety net for a bypassed check.
func Effective(flag string, current Mode) (Mode, error) {
	if flag != "" {
		switch Mode(flag) {
		case ModeAlways, ModeOptIn:
			return Mode(flag), nil
		}

		return "", fmt.Errorf("invalid project mode %q", flag)
	}
	if current == ModeAlways || current == ModeOptIn {
		return current, nil
	}

	return ModeAlways, nil
}

func ValidateFlag(flag string) error {
	if flag == "" {
		return nil
	}
	switch Mode(flag) {
	case ModeAlways, ModeOptIn:
		return nil
	}

	return errors.New("invalid --project-mode value, expected 'always' or 'opt-in'")
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
