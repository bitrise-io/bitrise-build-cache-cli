//go:build unit

package buildrequest_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildrequest"
)

func TestLoad_BuildSubtreeParametersShape(t *testing.T) {
	dd := t.TempDir()
	hex := "a1b2c3d4e5f6071829304152637485a0"
	dir := filepath.Join(dd, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"), []byte(
		`{"parameters":{"action":"build","qos":"default","command":"xcodebuild"}}`,
	), 0o644))

	info, err := buildrequest.Load(dd, hex)
	require.NoError(t, err)
	assert.Equal(t, "build", info.Action)
	assert.Equal(t, "default", info.QoS)
	assert.Equal(t, "xcodebuild", info.Command)
}

func TestLoad_IndexBuildSubtreeFallback(t *testing.T) {
	dd := t.TempDir()
	hex := "a1b2c3d4e5f6071829304152637485a0"
	dir := filepath.Join(dd, "Index.noindex", "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"), []byte(
		`{"action":"indexbuild","qos":"prepareForIndexing"}`,
	), 0o644))

	info, err := buildrequest.Load(dd, hex)
	require.NoError(t, err)
	assert.Equal(t, "indexbuild", info.Action)
	assert.Equal(t, "prepareForIndexing", info.QoS)
}

func TestLoad_FlatShape(t *testing.T) {
	dd := t.TempDir()
	hex := "a1b2c3d4e5f6071829304152637485a0"
	dir := filepath.Join(dd, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"), []byte(
		`{"buildRequest":{"action":"archive"}}`,
	), 0o644))

	info, err := buildrequest.Load(dd, hex)
	require.NoError(t, err)
	assert.Equal(t, "archive", info.Action)
}

func TestLoad_MissingReturnsErrNotFound(t *testing.T) {
	dd := t.TempDir()

	_, err := buildrequest.Load(dd, "a1b2c3d4e5f6071829304152637485a0")
	require.ErrorIs(t, err, buildrequest.ErrNotFound)
}

func TestLoad_EmptyInputs(t *testing.T) {
	_, err := buildrequest.Load("", "hex")
	require.Error(t, err)
	assert.False(t, errors.Is(err, buildrequest.ErrNotFound))

	_, err = buildrequest.Load("/tmp", "")
	require.Error(t, err)
}

func TestLoad_MalformedJSONReturnsDecodeError(t *testing.T) {
	dd := t.TempDir()
	hex := "a1b2c3d4e5f6071829304152637485a0"
	dir := filepath.Join(dd, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"), []byte(`not json`), 0o644))

	_, err := buildrequest.Load(dd, hex)
	require.Error(t, err)
	assert.False(t, errors.Is(err, buildrequest.ErrNotFound))
}
