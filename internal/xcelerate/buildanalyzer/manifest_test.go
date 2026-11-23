//go:build unit

package buildanalyzer_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

// synthManifest returns a minimal well-formed manifest.json body. The cases
// use Compile Foo.swift / Ld Foo as the smallest-possible shell+linker pair
// — enough to exercise description match, signature match, and the
// input/output file-path join that derives dependency edges.
const synthManifest = `{
  "client": {"name":"basic","version":0},
  "targets": {"": ["<all>"]},
  "nodes": {},
  "commands": {
    "<all>": {"tool":"phony","inputs":["x"],"outputs":["<all>"]},
    "P0:::CompileSwift": {
      "tool":"swift-driver-compilation",
      "description":"CompileSwift Foo.swift",
      "signature":"abc123",
      "inputs":["Foo.swift"],
      "outputs":["Foo.o"],
      "args":["swiftc","Foo.swift"]
    },
    "P2:target-X:Debug:Ld Foo": {
      "tool":"linker",
      "description":"Ld /Foo normal arm64",
      "inputs":["Foo.o"],
      "outputs":["/Foo"],
      "args":["ld","-o","/Foo"]
    }
  }
}`

func TestReadManifest_DecodesCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(path, []byte(synthManifest), 0o644))

	m, err := buildanalyzer.ReadManifest(path)
	require.NoError(t, err)
	require.Len(t, m.Commands, 3)

	cs := m.Commands["P0:::CompileSwift"]
	assert.Equal(t, "swift-driver-compilation", cs.Tool)
	assert.Equal(t, "CompileSwift Foo.swift", cs.Description)
	assert.Equal(t, "abc123", cs.Signature)
	assert.Equal(t, []string{"Foo.swift"}, cs.Inputs)
	assert.Equal(t, []string{"Foo.o"}, cs.Outputs)

	phony := m.Commands["<all>"]
	assert.Equal(t, "phony", phony.Tool)
	assert.Empty(t, phony.Description, "phony orchestration commands omit description")
}

func TestReadManifest_MissingFile(t *testing.T) {
	_, err := buildanalyzer.ReadManifest(filepath.Join(t.TempDir(), "nope.json"))
	require.Error(t, err)
}

func TestReadManifest_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	_, err := buildanalyzer.ReadManifest(path)
	require.Error(t, err)
}

func TestLocateManifest_PicksNewestXCBuildData(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "Build", "Intermediates.noindex", "XCBuildData")
	older := filepath.Join(root, "aaa.xcbuilddata")
	newer := filepath.Join(root, "bbb.xcbuilddata")
	require.NoError(t, os.MkdirAll(older, 0o755))
	require.NoError(t, os.MkdirAll(newer, 0o755))

	olderManifest := filepath.Join(older, "manifest.json")
	newerManifest := filepath.Join(newer, "manifest.json")
	require.NoError(t, os.WriteFile(olderManifest, []byte("{}"), 0o644))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(newerManifest, []byte("{}"), 0o644))

	path, err := buildanalyzer.LocateManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, newerManifest, path)
}

func TestLocateManifest_NoXCBuildDataDir(t *testing.T) {
	path, err := buildanalyzer.LocateManifest(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, path)
}

func TestLocateManifest_EmptyXCBuildDataDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Build", "Intermediates.noindex", "XCBuildData"), 0o755))

	path, err := buildanalyzer.LocateManifest(dir)
	require.NoError(t, err)
	assert.Empty(t, path)
}
