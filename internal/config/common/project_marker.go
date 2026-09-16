package common

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type ProjectMarker struct{}

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

	return &marker, nil
}

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
