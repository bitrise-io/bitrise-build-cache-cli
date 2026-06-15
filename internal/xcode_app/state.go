package xcode_app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// State captures what `xcode-app enable` did to the user's environment so
// `xcode-app disable` can undo it cleanly. Persisted as JSON next to the
// override xcconfig.
type State struct {
	// PreviousXCConfigPath is the value of XCODE_XCCONFIG_FILE at enable
	// time, captured BEFORE we overwrote it via `launchctl setenv`. On
	// disable we re-`setenv` to this value if non-empty, or `unsetenv` if
	// empty.
	PreviousXCConfigPath string `json:"previousXCConfigPath,omitempty"`
}

// LoadState reads the state file. Missing file returns the zero value + false
// + nil error so the caller can treat "never enabled" as a normal disable
// no-op rather than a hard failure.
func LoadState(path string) (State, bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // we control the path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{}, false, nil
		}

		return State{}, false, fmt.Errorf("read state file %s: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, false, fmt.Errorf("decode state file %s: %w", path, err)
	}

	return s, true, nil
}

// SaveState writes the state file atomically (write-temp + rename) so a
// crash mid-write never leaves a half-written JSON behind.
func SaveState(path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}

	return nil
}

// RemoveState deletes the state file. Missing file is not an error — disable
// is idempotent.
func RemoveState(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove state file %s: %w", path, err)
	}

	return nil
}
