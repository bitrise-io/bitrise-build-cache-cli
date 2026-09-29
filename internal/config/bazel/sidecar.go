package bazelconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

// Sidecar is the bazel activation provenance file written next to ~/.bazelrc.
// Drives the refresh nudge — the bazelrc itself is shared with the user's
// hand-written config so a sidecar is the only safe version source.
type Sidecar struct {
	ConfigVersion string    `json:"configVersion,omitempty"`
	WrittenAt     time.Time `json:"writtenAt,omitzero"`

	BazelrcPath       string `json:"bazelrcPath,omitempty"`
	CacheEnabled      bool   `json:"cacheEnabled,omitempty"`
	CachePushEnabled  bool   `json:"cachePushEnabled,omitempty"`
	BESEnabled        bool   `json:"besEnabled,omitempty"`
	RBEEnabled        bool   `json:"rbeEnabled,omitempty"`
	TimestampsEnabled bool   `json:"timestampsEnabled,omitempty"`
}

const (
	bazelToolName   = "bazel"
	sidecarFileName = "config.json"
)

// SidecarDirPath returns the directory the sidecar lives in.
func SidecarDirPath(home string) string {
	return paths.FromHome(home).BitriseCacheDir(bazelToolName)
}

// SidecarFilePath returns the absolute path of the bazel sidecar.
func SidecarFilePath(home string) string {
	return paths.FromHome(home).BitriseCacheFile(bazelToolName, sidecarFileName)
}

// WriteSidecar persists the bazel sidecar for the given home.
func WriteSidecar(home string, s Sidecar) error {
	s.ConfigVersion = toolconfig.BazelConfigVersion
	s.WrittenAt = time.Now().UTC()

	dir := SidecarDirPath(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create bazel sidecar dir: %w", err)
	}

	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bazel sidecar: %w", err)
	}

	target := SidecarFilePath(home)
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp bazel sidecar: %w", err)
	}

	defer func() {
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temp bazel sidecar: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp bazel sidecar: %w", err)
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("rename temp bazel sidecar to %s: %w", target, err)
	}

	return nil
}

// ReadSidecar loads the sidecar, returning (zero, false, nil) when the file is missing.
func ReadSidecar(home string) (Sidecar, bool, error) {
	path := SidecarFilePath(home)

	body, err := os.ReadFile(path) //nolint:gosec // path derived from home + constant
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Sidecar{}, false, nil
		}

		return Sidecar{}, false, fmt.Errorf("read bazel sidecar %s: %w", path, err)
	}

	var s Sidecar
	if err := json.Unmarshal(body, &s); err != nil {
		return Sidecar{}, false, fmt.Errorf("decode bazel sidecar %s: %w", path, err)
	}

	return s, true, nil
}
