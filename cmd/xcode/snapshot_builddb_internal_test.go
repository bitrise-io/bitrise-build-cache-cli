//go:build unit

package xcode

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestRunSnapshotBuildDB_RequiresDerivedData(t *testing.T) {
	prev := snapshotBuildDBFlagDerivedData
	t.Cleanup(func() { snapshotBuildDBFlagDerivedData = prev })

	snapshotBuildDBFlagDerivedData = ""

	err := runSnapshotBuildDB(&bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--derived-data")
}

func TestRunSnapshotBuildDB_SoftFailOnMissingDB(t *testing.T) {
	prev := snapshotBuildDBFlagDerivedData
	prevRoot := snapshotBuildDBFlagSnapshotRoot
	t.Cleanup(func() {
		snapshotBuildDBFlagDerivedData = prev
		snapshotBuildDBFlagSnapshotRoot = prevRoot
	})

	// DerivedData with no XCBuildData → soft-fail.
	snapshotBuildDBFlagDerivedData = t.TempDir()
	snapshotBuildDBFlagSnapshotRoot = t.TempDir()

	var out, stderr bytes.Buffer
	require.NoError(t, runSnapshotBuildDB(&out, &stderr))
	assert.Empty(t, out.String())
	assert.Contains(t, stderr.String(), "no build.db")
}

// TestRunSnapshotBuildDB_SnapshotsToOverrideRoot wires a tiny source DB under
// the expected DerivedData shape, points the flag at a tempdir store, and
// asserts the subcommand prints the resulting snapshot path.
func TestRunSnapshotBuildDB_SnapshotsToOverrideRoot(t *testing.T) {
	prev := snapshotBuildDBFlagDerivedData
	prevRoot := snapshotBuildDBFlagSnapshotRoot
	t.Cleanup(func() {
		snapshotBuildDBFlagDerivedData = prev
		snapshotBuildDBFlagSnapshotRoot = prevRoot
	})

	derivedData := t.TempDir()
	hex := "d3b07384d113edec49eaa6238ad5ff00"
	xcbuildDir := filepath.Join(derivedData, "Build", "Intermediates.noindex", "XCBuildData")
	require.NoError(t, os.MkdirAll(filepath.Join(xcbuildDir, hex+".xcbuilddata"), 0o755))

	seedTinyLlbuildDB(t, filepath.Join(xcbuildDir, "build.db"), 3)

	storeRoot := t.TempDir()
	snapshotBuildDBFlagDerivedData = derivedData
	snapshotBuildDBFlagSnapshotRoot = storeRoot

	var out, stderr bytes.Buffer
	require.NoError(t, runSnapshotBuildDB(&out, &stderr))

	path := strings.TrimSpace(out.String())
	assert.FileExists(t, path)
	assert.True(t, strings.HasPrefix(path, filepath.Join(storeRoot, hex)),
		"snapshot path should sit under <storeRoot>/<hex>/: %s", path)
}

// seedTinyLlbuildDB writes the minimum llbuild-schema DB the snapshot flow
// needs: one info row with the given iteration + one rule row. Mirrors the
// snapshotstore_test helper so this file stays self-contained.
func seedTinyLlbuildDB(t *testing.T, path string, iteration int64) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const schema = `
CREATE TABLE info (id INTEGER PRIMARY KEY, version INTEGER, client_version INTEGER, iteration INTEGER);
CREATE TABLE key_names (id INTEGER PRIMARY KEY, key STRING UNIQUE);
CREATE TABLE rule_results (key_id INTEGER PRIMARY KEY, value BLOB, signature INTEGER, built_at INTEGER, computed_at INTEGER, start REAL, end REAL, dependencies BLOB);
`
	_, err = db.Exec(schema)
	require.NoError(t, err)

	_, err = db.Exec("INSERT INTO info(id, version, client_version, iteration) VALUES (0, 17, 9, ?)", iteration)
	require.NoError(t, err)

	_, err = db.Exec("INSERT INTO key_names(id, key) VALUES (1, 'CA')")
	require.NoError(t, err)

	_, err = db.Exec("INSERT INTO rule_results(key_id, value, signature, built_at, computed_at, start, end, dependencies) VALUES (1, ?, 42, ?, ?, 0, 0.01, NULL)", []byte{0}, iteration, iteration)
	require.NoError(t, err)
}
