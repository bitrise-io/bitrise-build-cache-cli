//go:build unit

package xcode_app_casinject

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const socket = "/tmp/xcelerate-proxy.sock"

func writeCasConfig(t *testing.T, dir string, body string) string {
	t.Helper()
	p := filepath.Join(dir, ".cas-config")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	return p
}

func TestInjectFile_AddsRemoteServiceWhenMissing(t *testing.T) {
	dir := t.TempDir()
	p := writeCasConfig(t, dir, `{"CASPath":"/some/path"}`)

	changed, err := InjectFile(p, socket)
	require.NoError(t, err)
	require.True(t, changed)

	raw, err := os.ReadFile(p)
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(raw, &obj))
	require.Equal(t, "/some/path", obj["CASPath"])

	rs, ok := obj["RemoteService"].(map[string]any)
	require.True(t, ok, "RemoteService object missing: %s", string(raw))
	require.Equal(t, socket, rs["Path"])
}

func TestInjectFile_IdempotentWhenSocketMatches(t *testing.T) {
	dir := t.TempDir()
	p := writeCasConfig(t, dir, `{"CASPath":"/x","RemoteService":{"Path":"`+socket+`"}}`)

	changed, err := InjectFile(p, socket)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestInjectFile_ReplacesDifferentRemoteService(t *testing.T) {
	dir := t.TempDir()
	p := writeCasConfig(t, dir, `{"CASPath":"/x","RemoteService":{"Path":"/old.sock"}}`)

	changed, err := InjectFile(p, socket)
	require.NoError(t, err)
	require.True(t, changed)

	raw, err := os.ReadFile(p)
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(raw, &obj))
	rs := obj["RemoteService"].(map[string]any) //nolint:forcetypeassert
	require.Equal(t, socket, rs["Path"])
}

func TestInjectFile_PreservesExtraKeys(t *testing.T) {
	dir := t.TempDir()
	p := writeCasConfig(t, dir, `{"CASPath":"/x","Extra":"keep","N":42}`)

	_, err := InjectFile(p, socket)
	require.NoError(t, err)

	raw, err := os.ReadFile(p)
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(raw, &obj))
	require.Equal(t, "keep", obj["Extra"])
	require.EqualValues(t, 42, obj["N"])
}

func TestInjectFile_MalformedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	p := writeCasConfig(t, dir, `{not json`)

	_, err := InjectFile(p, socket)
	require.Error(t, err)
}

func TestIsCasConfigPath(t *testing.T) {
	require.True(t, IsCasConfigPath("/a/b/.cas-config"))
	require.False(t, IsCasConfigPath("/a/b/other.json"))
}
