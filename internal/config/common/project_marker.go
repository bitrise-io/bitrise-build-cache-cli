package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ProjectMarker is the parsed content of a per-project .bitrise-build-cache.json file.
// Pointer bools distinguish absent from explicit false.
type ProjectMarker struct {
	Workspace string                       `json:"workspace"`
	Push      *bool                        `json:"push,omitempty"`
	Tools     map[string]ProjectMarkerTool `json:"tools,omitempty"`
}

type ProjectMarkerTool struct {
	Enabled *bool `json:"enabled,omitempty"`
}

var ErrProjectMarkerMissingWorkspace = errors.New("project marker missing required field: workspace")

// ReadProjectMarker reads and validates a marker file.
// Returns (nil, nil) when the file does not exist.
func ReadProjectMarker(path string, osProxy utils.OsProxy) (*ProjectMarker, error) {
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

	if marker.Workspace == "" {
		return nil, fmt.Errorf("%s: %w", path, ErrProjectMarkerMissingWorkspace)
	}

	return &marker, nil
}

// WalkUpFindMarker walks up from startDir looking for a marker file.
// Returns ("", nil, nil) when no marker is found up to the filesystem root.
// Symlinks are not resolved.
func WalkUpFindMarker(startDir string, osProxy utils.OsProxy) (string, *ProjectMarker, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, paths.ProjectMarkerFilename)
		marker, err := ReadProjectMarker(candidate, osProxy)
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
