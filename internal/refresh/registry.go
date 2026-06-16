// Package refresh tracks which build-tool configs the CLI has generated and surfaces a refresh nudge when CLI version drifts.
package refresh

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const StateDirRelative = ".local/state/bitrise-build-cache"

const RegistryFile = "refresh-registry.json"

// Tool identifiers — constants so typos at Mark() sites fail to compile.
const (
	ToolGradle    = "gradle"
	ToolBazel     = "bazel"
	ToolXcelerate = "xcelerate"
	ToolCcache    = "ccache"
)

type Entry struct {
	Tool         string    `json:"tool"`
	ConfigPath   string    `json:"config_path,omitempty"`
	CLIVersion   string    `json:"cli_version"`
	RegisteredAt time.Time `json:"registered_at"`
}

type Registry struct {
	Entries map[string]Entry `json:"entries"`
}

func registryPath(home string) string {
	return filepath.Join(home, StateDirRelative, RegistryFile)
}

func Load(home string) (Registry, error) {
	path := registryPath(home)

	body, err := os.ReadFile(path) //nolint:gosec // home + constant
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Registry{Entries: map[string]Entry{}}, nil
		}

		return Registry{}, fmt.Errorf("read refresh registry %s: %w", path, err)
	}

	var reg Registry
	if err := json.Unmarshal(body, &reg); err != nil {
		return Registry{}, fmt.Errorf("parse refresh registry %s: %w", path, err)
	}

	if reg.Entries == nil {
		reg.Entries = map[string]Entry{}
	}

	return reg, nil
}

// Save writes atomically (write-temp + rename).
func Save(home string, reg Registry) error {
	dir := filepath.Join(home, StateDirRelative)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	body, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal refresh registry: %w", err)
	}

	target := registryPath(home)
	tmp, err := os.CreateTemp(dir, ".refresh-registry-*.json")
	if err != nil {
		return fmt.Errorf("create temp registry file: %w", err)
	}

	defer func() {
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temp registry: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp registry: %w", err)
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("rename temp registry to %s: %w", target, err)
	}

	return nil
}

// Mark is RMW under an advisory flock so parallel activates don't race on Save's last-write-wins rename.
func Mark(home, tool, configPath, cliVersion string) error {
	unlock, err := lockRegistry(home)
	if err != nil {
		return err
	}

	defer unlock()

	reg, err := Load(home)
	if err != nil {
		return err
	}

	reg.Entries[tool] = Entry{
		Tool:         tool,
		ConfigPath:   configPath,
		CLIVersion:   cliVersion,
		RegisteredAt: time.Now(),
	}

	return Save(home, reg)
}

// SortedEntries returns alphabetical order so notify text is reproducible.
func (r Registry) SortedEntries() []Entry {
	out := make([]Entry, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })

	return out
}
