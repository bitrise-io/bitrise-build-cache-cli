package versioncheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

const StateFile = "version-state.json"

type State struct {
	LastVersion string    `json:"last_version"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	LastNudgeAt time.Time `json:"last_nudge_at,omitzero"`
}

func statePath(home string) string {
	return paths.FromHome(home).StateFile(StateFile)
}

// LoadState returns zero State + nil when the file is missing (first-run case).
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

// SaveState writes atomically (write-temp + rename). Last-write-wins on parallel invocations.
func SaveState(home string, st State) error {
	dir := paths.FromHome(home).StateDir()
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
