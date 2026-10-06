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

// ProjectOptedOut reports whether the current project should be treated as
// opted-out of analytics: machine ProjectMode is opt-in AND no project marker
// is found walking up from the caller's cwd.
//
// Caller's cwd MUST be the project root. Daemons spawned via the
// detached-spawn convention inherit the client's cwd (see
// pkg/ccache/storage_helper.go) — safe to call from such daemons. Not safe
// to call from long-lived daemons started independently of a build (e.g.
// xcelerate proxy's Enricher) — they must resolve project dir separately.
//
// Any read/config error returns true (silent-suppress): a failure to prove
// opt-in explicitly is treated as opted-out. Matches cmd/xcode.projectModeGates.
func ProjectOptedOut(osProxy utils.OsProxy, logger log.Logger) bool {
	p, err := paths.Default()
	if err != nil {
		return false
	}

	current, err := Read(osProxy, p, logger)
	if err != nil {
		return false
	}
	if ResolvedProjectMode(current) != ModeOptIn {
		return false
	}

	cwd, err := osProxy.Getwd()
	if err != nil {
		return true
	}

	found, _, err := FindMarker(cwd, osProxy)
	if err != nil {
		return true
	}

	return !found
}
