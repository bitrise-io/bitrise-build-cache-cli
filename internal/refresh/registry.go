// Package refresh tracks which build-tool configs the CLI has generated
// (gradle / bazel / xcelerate / ccache) and surfaces a refresh-needed nudge
// when the running CLI version drifts from the version that wrote the config.
//
// Phase 1 (this PR, ACI-5039): detection + notify only. On Bump the user
// sees the exact `bitrise-build-cache activate <tool>` commands to rerun.
//
// Phase 2 (deferred): replay activate programmatically from registered
// state so the user doesn't have to run anything. Requires per-tool replay
// handlers that can reconstruct activate args from persisted config /
// keychain / env — non-trivial and intentionally out of scope here.
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

// StateDirRelative is the path beneath the user's home where refresh state
// lives. Same root as versioncheck (ACI-5037) so all CLI-managed local state
// sits under one tree.
const StateDirRelative = ".local/state/bitrise-build-cache"

// RegistryFile is the basename of the on-disk registry.
const RegistryFile = "refresh-registry.json"

// Tool identifiers. Constants instead of free-form strings so accidental
// typos at Mark() sites fail to compile.
const (
	ToolGradle    = "gradle"
	ToolBazel     = "bazel"
	ToolXcelerate = "xcelerate"
	ToolCcache    = "ccache"
)

// Entry is one tool's registration record.
type Entry struct {
	// Tool is one of the Tool* constants.
	Tool string `json:"tool"`
	// ConfigPath is the file the activate command produced — included in the
	// nudge text so the user can see what would be refreshed.
	ConfigPath string `json:"config_path,omitempty"`
	// CLIVersion is the CLI version that wrote the entry.
	CLIVersion string `json:"cli_version"`
	// RegisteredAt is when the entry was written. Updated on every activate
	// rerun so stale entries surface in observability.
	RegisteredAt time.Time `json:"registered_at"`
}

// Registry is the on-disk shape.
type Registry struct {
	Entries map[string]Entry `json:"entries"`
}

// NOTE on stale-entry pruning: an entry written by ACI-5034 (gradle) then
// abandoned (user moved off Gradle, never reran `activate gradle`) lingers
// forever in the registry, and `OnBump` will keep nudging them about
// refreshing it. The fix is to prune entries whose ConfigPath no longer
// exists on disk — cheap to do at Load() time. Tracked as follow-up; not
// blocking M1 because the nudge is a one-line stderr write per CLI
// invocation with a 24h cooldown elsewhere (see versioncheck.NudgeCooldown),
// so the wrong-nudge cost is low.

func registryPath(home string) string {
	return filepath.Join(home, StateDirRelative, RegistryFile)
}

// Load reads the registry. Missing file is the "no tools registered yet"
// case and returns an empty Registry without error.
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

// Save writes the registry atomically (write-temp + rename) so a crash
// mid-write can't corrupt the JSON.
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

// Mark records (or updates) the entry for one tool. Used by the activate
// subcommands so D1's bump detection knows what was previously configured.
// Failures are returned but callers MUST treat them as advisory — failing
// to write the registry should not fail an otherwise-successful activate.
//
// Concurrency: Mark is read-modify-write, so two parallel CLI invocations
// activating different tools at the same time would otherwise race and one
// entry would be lost on Save (last-write-wins under the rename). We take
// an OS-level advisory lock on a sibling lockfile around the whole RMW so
// the second invocation blocks briefly, sees the first's commit, and
// merges its own delta on top.
//
// flock with LOCK_EX is portable across macOS + Linux (both POSIX) and
// the only consumers of this registry are CLI processes on the same host,
// so we don't need a cross-host coordinator.
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

// SortedEntries returns the registry entries in stable, alphabetical order
// for human-readable output. Map iteration order isn't deterministic; this
// keeps the notify text reproducible (good for tests too).
func (r Registry) SortedEntries() []Entry {
	out := make([]Entry, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })

	return out
}
