package buildanalyzer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Manifest is the decoded `manifest.json` written by Xcode's build system
// alongside build.db in `<build-hex>.xcbuilddata/`.
//
// Deviations from docs/xcode-build-analyzer.md (written before a real sample
// was inspected): the file is **JSON, not YAML**, and the filename is
// `manifest.json`, not `manifest-*.xcbuild`. The structural shape under
// `commands` still matches llbuild's model — each entry carries tool, inputs,
// outputs, description, args, signature — but only shell/linker/compiler
// commands populate every field. Orchestration commands (phony,
// stale-file-removal, create-build-directory, phony aggregation nodes under
// `<target-…>` keys) carry a bare `{tool, inputs?, outputs?}` subset.
type Manifest struct {
	// Commands is keyed by the stable llbuild command name — a string that
	// typically begins with a priority prefix (`P0:::`, `P2:target-<guid>:Debug:`)
	// followed by the same text llbuild emits in `description`. The map value
	// carries the planned-task payload.
	Commands map[string]ManifestCommand `json:"commands"`
}

// ManifestCommand is one planned task inside `commands`. Fields that
// orchestration entries omit (description, args, signature) stay empty on
// those entries; v1's join only consumes entries that carry a description.
type ManifestCommand struct {
	Tool        string   `json:"tool"`
	Description string   `json:"description,omitempty"`
	Signature   string   `json:"signature,omitempty"`
	Inputs      []string `json:"inputs,omitempty"`
	Outputs     []string `json:"outputs,omitempty"`
	Args        []string `json:"args,omitempty"`
}

// ReadManifest decodes a manifest.json file into a Manifest. Returns a
// wrapped fs.ErrNotExist on missing files so callers can soft-fail.
func ReadManifest(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	var m Manifest
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
	}

	return m, nil
}

// LocateManifest finds the `manifest.json` under
// `<derivedData>/Build/Intermediates.noindex/XCBuildData/<hex>.xcbuilddata/`.
// Multiple xcbuilddata directories can co-exist (one per build configuration);
// the newest by mtime wins — same policy MostRecentActivityLog follows.
// Returns ("", nil) when the XCBuildData dir exists but holds no candidate.
func LocateManifest(derivedData string) (string, error) {
	root := filepath.Join(derivedData, "Build", "Intermediates.noindex", "XCBuildData")

	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("read %s: %w", root, err)
	}

	type candidate struct {
		path  string
		mtime int64
	}

	cands := make([]candidate, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".xcbuilddata" {
			continue
		}

		manifestPath := filepath.Join(root, e.Name(), "manifest.json")
		info, err := os.Stat(manifestPath)
		if err != nil {
			continue
		}

		cands = append(cands, candidate{path: manifestPath, mtime: info.ModTime().UnixNano()})
	}

	if len(cands) == 0 {
		return "", nil
	}

	sort.Slice(cands, func(i, j int) bool { return cands[i].mtime > cands[j].mtime })

	return cands[0].path, nil
}
