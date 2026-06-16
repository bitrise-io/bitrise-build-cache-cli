package xcode_app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

type State struct {
	PreviousXCConfigPath string `json:"previousXCConfigPath,omitempty"`
}

// LoadState returns zero + false + nil for a missing file (caller treats as never enabled).
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

// SaveState writes atomically (write-temp + rename).
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

func RemoveState(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove state file %s: %w", path, err)
	}

	return nil
}
