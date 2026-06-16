// Package versioncheck detects CLI version drift across runs and (optionally)
// fetches the latest release tag from GitHub to nudge the user when their
// installed CLI is behind. State is persisted under
// ~/.local/state/bitrise-build-cache/ so it survives invocations.
package versioncheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// StateDirRelative is the path beneath the user's home where the version state file lives.
const StateDirRelative = ".local/state/bitrise-build-cache"

// StateFile is the basename of the persisted version state.
const StateFile = "version-state.json"

// State is the on-disk shape persisted between runs.
type State struct {
	// LastVersion is the CLI version that wrote this file. Compared against
	// the running binary's version to detect a bump.
	LastVersion string `json:"last_version"`
	// LastSeenAt is when the running binary's version was last observed.
	// Updated on every CLI invocation regardless of bump.
	LastSeenAt time.Time `json:"last_seen_at"`
	// LastNudgeAt is when the user was last nudged about a behind-brew-latest
	// release. Used to throttle the GitHub-release lookup to at most once
	// every NudgeCooldown.
	LastNudgeAt time.Time `json:"last_nudge_at,omitzero"`
}

// statePath resolves the absolute path of the state file under home.
func statePath(home string) string {
	return filepath.Join(home, StateDirRelative, StateFile)
}

// LoadState reads the persisted state from disk. Returns a zero State + nil
// error when the file doesn't exist — that's the "first run" case and is
// expected. Any other read / parse error propagates.
func LoadState(home string) (State, error) {
	path := statePath(home)

	body, err := os.ReadFile(path) //nolint:gosec // path is derived from home + a constant
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{}, nil
		}

		return State{}, fmt.Errorf("read version state %s: %w", path, err)
	}

	var st State
	if err := json.Unmarshal(body, &st); err != nil {
		return State{}, fmt.Errorf("parse version state %s: %w", path, err)
	}

	return st, nil
}

// SaveState writes the state atomically (write-to-temp + rename) so a crash
// between truncate and write can't leave the file half-written. State is
// benign and last-write-wins on parallel CLI invocations; no file locking.
func SaveState(home string, st State) error {
	dir := filepath.Join(home, StateDirRelative)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	body, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal version state: %w", err)
	}

	target := statePath(home)
	tmp, err := os.CreateTemp(dir, ".version-state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}

	defer func() {
		// Best-effort: if rename below succeeded the temp is already gone, so
		// this is a no-op error.
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temp state file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("rename temp state to %s: %w", target, err)
	}

	return nil
}
