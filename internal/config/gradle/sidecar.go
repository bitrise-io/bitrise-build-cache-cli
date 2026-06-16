package gradleconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/toolconfig"
)

// Sidecar is the gradle activation provenance file written next to the
// generated init.d script. Drives the refresh nudge — the init.d script
// itself is not JSON, so this sidecar is the canonical version source.
type Sidecar struct {
	CLIVersion    string    `json:"cliVersion,omitempty"`
	ConfigVersion string    `json:"configVersion,omitempty"`
	WrittenAt     time.Time `json:"writtenAt,omitzero"`

	// InitScriptPath is the generated ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts path.
	InitScriptPath string `json:"initScriptPath,omitempty"`
	// CacheEnabled mirrors the --cache flag at activate time.
	CacheEnabled bool `json:"cacheEnabled,omitempty"`
	// CachePushEnabled mirrors the --cache-push flag.
	CachePushEnabled bool `json:"cachePushEnabled,omitempty"`
	// AnalyticsEnabled mirrors the --analytics flag.
	AnalyticsEnabled bool `json:"analyticsEnabled,omitempty"`
}

const (
	sidecarDirRelative = ".bitrise/cache/gradle"
	sidecarFileName    = "config.json"
)

// SidecarDirPath returns the directory the sidecar lives in.
func SidecarDirPath(home string) string {
	return filepath.Join(home, sidecarDirRelative)
}

// SidecarFilePath returns the absolute path of the gradle sidecar.
func SidecarFilePath(home string) string {
	return filepath.Join(SidecarDirPath(home), sidecarFileName)
}

// WriteSidecar persists the gradle sidecar for the given home.
func WriteSidecar(home string, s Sidecar) error {
	s.CLIVersion = configcommon.GetCLIVersion(nil)
	s.ConfigVersion = toolconfig.GradleConfigVersion
	s.WrittenAt = time.Now().UTC()

	dir := SidecarDirPath(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create gradle sidecar dir: %w", err)
	}

	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal gradle sidecar: %w", err)
	}

	target := SidecarFilePath(home)
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp gradle sidecar: %w", err)
	}

	defer func() {
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temp gradle sidecar: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp gradle sidecar: %w", err)
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("rename temp gradle sidecar to %s: %w", target, err)
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

		return Sidecar{}, false, fmt.Errorf("read gradle sidecar %s: %w", path, err)
	}

	var s Sidecar
	if err := json.Unmarshal(body, &s); err != nil {
		return Sidecar{}, false, fmt.Errorf("decode gradle sidecar %s: %w", path, err)
	}

	return s, true, nil
}
