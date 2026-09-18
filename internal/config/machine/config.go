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
	ProjectMode Mode  `json:"project_mode,omitempty"`
	CachePush   *bool `json:"cache_push,omitempty"`
}

// DefaultCachePush is the fallback when neither the flag nor the machine
// config carries a stored preference. Push is on by default; the resolver
// falls back to it when there is nothing better to consult.
const DefaultCachePush = true

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

// EffectiveProjectMode picks the mode a tool should honour given an explicit
// flag value (empty when unset) and the mode currently on disk. Empty flag +
// empty current falls back to ModeAlways so a machine without a stored
// preference keeps the prior behavior. An unknown flag returns an error —
// callers are expected to pre-validate with ValidateProjectModeFlag, so this is
// a safety net for a bypassed check.
func EffectiveProjectMode(flag string, current Mode) (Mode, error) {
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

// EffectiveCachePush picks the push value a tool should honour given whether
// the caller explicitly set --cache-push, the flag's value, and whatever is
// currently persisted. Precedence: explicit flag → stored preference →
// DefaultCachePush. Nil stored means "no preference recorded".
func EffectiveCachePush(flagChanged bool, flagValue bool, current *bool) bool {
	if flagChanged {
		return flagValue
	}
	if current != nil {
		return *current
	}

	return DefaultCachePush
}

// StoredCachePush reads the machine config and returns the effective push
// value with no CLI override, falling back to DefaultCachePush when nothing
// is recorded.
func StoredCachePush(osProxy utils.OsProxy, p paths.Paths, logger log.Logger) (bool, error) {
	cfg, err := Read(osProxy, p, logger)
	if err != nil {
		return DefaultCachePush, err
	}

	return EffectiveCachePush(false, false, cfg.CachePush), nil
}

// StoredProjectMode reads the machine config and returns the effective mode
// with no CLI override. An empty stored value resolves to ModeAlways.
func StoredProjectMode(osProxy utils.OsProxy, p paths.Paths, logger log.Logger) (Mode, error) {
	cfg, err := Read(osProxy, p, logger)
	if err != nil {
		return "", err
	}
	if cfg.ProjectMode == "" {
		return ModeAlways, nil
	}

	return cfg.ProjectMode, nil
}

func ValidateProjectModeFlag(flag string) error {
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
