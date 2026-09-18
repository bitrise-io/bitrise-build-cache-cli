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

// DefaultCachePush is the fallback when neither the flag nor the machine
// config carries a stored preference. Push is on by default; the resolver
// falls back to it when there is nothing better to consult.
const DefaultCachePush = true

// FlagOverlay carries the CLI-flag inputs that override the on-disk machine
// config. Zero value for a field means "no flag set for this run".
type FlagOverlay struct {
	// ProjectMode is empty when the flag was not set.
	ProjectMode string
	// CachePush is nil when the flag was not set (tri-state: nil / &true / &false).
	CachePush *bool
}

// Source labels where a resolved field came from — surfaced by Effective so
// doctor and similar inspectors can report provenance without re-reading the
// config file.
const (
	SourceFlag          = "flag"
	SourceMachineConfig = "machine config"
	SourceDefault       = "default"
)

// Sources reports, per field, whether the value Effective returned came from
// the overlay, the stored config, or the built-in default.
type Sources struct {
	ProjectMode string
	CachePush   string
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

// Effective resolves the machine-wide config for the current run using
// overlay > stored > default precedence, and returns a fully-populated Config
// alongside a Sources record naming the origin of each field. An unknown
// overlay ProjectMode returns an error — the resolver is the single validator.
func Effective(overlay FlagOverlay, current Config) (Config, Sources, error) {
	mode, modeSource, err := effectiveProjectMode(overlay.ProjectMode, current.ProjectMode)
	if err != nil {
		return Config{}, Sources{}, err
	}

	push, pushSource := effectiveCachePush(overlay.CachePush, current.CachePush)

	return Config{
			ProjectMode: mode,
			CachePush:   &push,
		}, Sources{
			ProjectMode: modeSource,
			CachePush:   pushSource,
		}, nil
}

func effectiveProjectMode(flag string, current Mode) (Mode, string, error) {
	if flag != "" {
		switch Mode(flag) {
		case ModeAlways, ModeOptIn:
			return Mode(flag), SourceFlag, nil
		}

		return "", "", fmt.Errorf("invalid project mode %q, expected 'always' or 'opt-in'", flag)
	}
	if current == ModeAlways || current == ModeOptIn {
		return current, SourceMachineConfig, nil
	}

	return ModeAlways, SourceDefault, nil
}

func effectiveCachePush(flag *bool, current *bool) (bool, string) {
	if flag != nil {
		return *flag, SourceFlag
	}
	if current != nil {
		return *current, SourceMachineConfig
	}

	return DefaultCachePush, SourceDefault
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
