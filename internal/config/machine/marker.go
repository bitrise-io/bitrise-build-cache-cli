package machine

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type ProjectMarker struct{}

// ReadMarker returns nil when the file is absent, an error on malformed JSON,
// and a non-nil marker on any valid body.
func ReadMarker(path string, osProxy utils.OsProxy) (*ProjectMarker, error) {
	content, exists, err := osProxy.ReadFileIfExists(path)
	if err != nil {
		return nil, fmt.Errorf("read project marker %s: %w", path, err)
	}
	if !exists {
		return nil, nil //nolint:nilnil // absent marker is a valid state
	}

	var marker ProjectMarker
	if err := json.Unmarshal([]byte(content), &marker); err != nil {
		return nil, fmt.Errorf("parse project marker %s: %w", path, err)
	}

	return &marker, nil
}

// WalkUpFindMarker walks up from startDir until it finds the marker or reaches
// the filesystem root. The typed return distinguishes "no marker" (marker=nil,
// err=nil) from "marker is malformed" (err != nil).
func WalkUpFindMarker(startDir string, osProxy utils.OsProxy) (string, *ProjectMarker, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, paths.ProjectMarkerFilename)
		marker, err := ReadMarker(candidate, osProxy)
		if err != nil {
			return "", nil, err
		}
		if marker != nil {
			return candidate, marker, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, nil
		}
		dir = parent
	}
}

// FindMarker is the boolean-only view of WalkUpFindMarker.
func FindMarker(startDir string, osProxy utils.OsProxy) (bool, string, error) {
	path, marker, err := WalkUpFindMarker(startDir, osProxy)
	if err != nil {
		return false, "", err
	}

	return marker != nil, path, nil
}

// EnsureMarker writes an empty marker at cwd when opt-in mode is active and no
// ancestor marker already covers this directory. Write failures are logged and
// swallowed so activation still succeeds.
func EnsureMarker(cwd string, mode Mode, osProxy utils.OsProxy, logger log.Logger) error {
	if mode != ModeOptIn {
		return nil
	}
	if cwd == "" {
		return nil
	}

	found, path, err := FindMarker(cwd, osProxy)
	if err != nil {
		if logger != nil {
			logger.Warnf("Skipping per-project marker creation: %s", err)
		}

		return nil
	}
	if found {
		if logger != nil {
			logger.Debugf("Per-project marker already covers %s (found at %s).", cwd, path)
		}

		return nil
	}

	target := filepath.Join(cwd, paths.ProjectMarkerFilename)
	if err := osProxy.WriteFile(target, []byte("{}\n"), 0o644); err != nil {
		if logger != nil {
			logger.Warnf("Could not write per-project marker at %s: %s", target, err)
		}

		return nil
	}

	if logger != nil {
		logger.TInfof("Wrote per-project marker at %s", target)
	}

	return nil
}
