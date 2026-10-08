// Package sessionfile persists the (anchor pid, start-ms) → slugs mapping
// used by the wrapper-less derivation path to exchange BitriseAppSlug /
// BitriseBuildSlug / BitriseStepSlug with the proxy. The file lives at
// ~/.bitrise-xcelerate/sessions/<anchor-pid>-<anchor-start-ms>.json.
//
// PR-C scaffold only: writer type + reader lookup, no caller writes yet. The
// proxy reader side (added in a follow-up) ignores a missing file and
// proceeds without slugs.
package sessionfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// SchemaVersion is the on-disk version; readers must leave files with a
// higher version on disk so a newer binary can pick them up.
const SchemaVersion = 1

// File is the (anchor pid, start-ms) → slugs mapping persisted on disk.
type File struct {
	SchemaVersion int    `json:"schema_version"`
	InvocationID  string `json:"invocation_id"`
	AppSlug       string `json:"app_slug"`
	BuildSlug     string `json:"build_slug"`
	StepSlug      string `json:"step_slug"`
}

// Write persists f under paths.SessionFilePath(pid, startMs) with an atomic
// tmp+rename. Mkdir failures and marshal failures return wrapped errors so
// callers can surface them at Debug level.
func Write(p paths.Paths, pid int, startMs int64, f File) error {
	if f.SchemaVersion == 0 {
		f.SchemaVersion = SchemaVersion
	}

	dst := p.SessionFilePath(pid, startMs)
	if err := os.MkdirAll(p.XcelerateSessionsAnchorsDir(), 0o755); err != nil {
		return fmt.Errorf("mkdir sessions dir: %w", err)
	}

	body, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal session file: %w", err)
	}

	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("rename %s: %w", dst, err)
	}

	return nil
}

// Read loads the session file for the given anchor. Returns a zero-value
// File + nil error when the file does not exist (missing is not an error at
// the derivation path).
func Read(p paths.Paths, pid int, startMs int64) (File, error) {
	body, err := os.ReadFile(p.SessionFilePath(pid, startMs))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return File{}, nil
		}

		return File{}, fmt.Errorf("read session file: %w", err)
	}

	var f File
	if err := json.Unmarshal(body, &f); err != nil {
		return File{}, fmt.Errorf("unmarshal session file: %w", err)
	}

	return f, nil
}
