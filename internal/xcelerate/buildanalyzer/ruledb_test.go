//go:build unit

package buildanalyzer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestReadBuildDB_InMemoryJoin builds the llbuild table layout in :memory:,
// inserts three key_names / rule_results rows and asserts ReadBuildDB joins
// them cleanly. The reader pulls `start` (REAL) and `signature` (INTEGER)
// alongside `built_at` (INTEGER iteration counter).
func TestReadBuildDB_InMemoryJoin(t *testing.T) {
	path := prepareTempDB(t, []insertRow{
		{keyID: 1, key: "CP0:::Compile Foo.swift", builtAt: 5, start: 100.5, signature: 7750609086073513320},
		{keyID: 2, key: "CP0:::Link Foo", builtAt: 4, start: 102.0, signature: 4762648219942629023},
		{keyID: 3, key: "N/tmp/Foo.swift", builtAt: 5, start: 100.1, signature: 1},
	}, 5)

	snap, err := ReadBuildDB(path)
	require.NoError(t, err)
	require.Equal(t, int64(5), snap.Iteration)
	require.Len(t, snap.Rules, 3)

	swift, ok := snap.Rules["CP0:::Compile Foo.swift"]
	require.True(t, ok)
	require.Equal(t, int64(5), swift.Iteration)
	require.NotEmpty(t, swift.Signature)
	require.Equal(t, 100.5, swift.BuiltAtStart)

	link, ok := snap.Rules["CP0:::Link Foo"]
	require.True(t, ok)
	require.Equal(t, int64(4), link.Iteration)
	require.NotEqual(t, swift.Signature, link.Signature)

	node, ok := snap.Rules["N/tmp/Foo.swift"]
	require.True(t, ok)
	require.Equal(t, int64(1), int64(node.Signature[0]))
}

// TestReadBuildDB_MissingInfoRow verifies the Iteration=0 fallback path:
// when `info` holds no row, the reader returns the rule map but leaves the
// iteration at its zero value for Classify to pick up.
func TestReadBuildDB_MissingInfoRow(t *testing.T) {
	path := prepareTempDB(t, []insertRow{
		{keyID: 1, key: "CA", builtAt: 1, start: 1.0, signature: 42},
	}, 0)

	snap, err := ReadBuildDB(path)
	require.NoError(t, err)
	require.Equal(t, int64(0), snap.Iteration)
	require.Len(t, snap.Rules, 1)
}

// TestLocateBuildDB_PresentAndAbsent exercises both the happy path and the
// soft-fail `("", nil)` contract LocateManifest also honors.
func TestLocateBuildDB_PresentAndAbsent(t *testing.T) {
	dir := t.TempDir()
	path, err := LocateBuildDB(dir)
	require.NoError(t, err)
	require.Empty(t, path)

	// Lay a dummy file at the expected location.
	target := filepath.Join(dir, "Build", "Intermediates.noindex", "XCBuildData", "build.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, nil, 0o600))

	path, err = LocateBuildDB(dir)
	require.NoError(t, err)
	require.Equal(t, target, path)
}

// Private — test helpers.

// insertRow mirrors a rule_results+key_names pair; signature is the raw int64
// SQLite will treat as an INTEGER column.
type insertRow struct {
	keyID     int64
	key       string
	builtAt   int64
	start     float64
	signature int64
}

// prepareTempDB sets up a file-backed sqlite DB with the llbuild schema,
// inserts rows, optionally seeds the info table (iteration > 0), and returns
// the on-disk path. The DB sits in t.TempDir so t.Cleanup disposes of it.
func prepareTempDB(t *testing.T, rows []insertRow, iteration int64) string {
	t.Helper()

	path := t.TempDir() + "/build.db"
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const schema = `
CREATE TABLE info (id INTEGER PRIMARY KEY, version INTEGER, client_version INTEGER, iteration INTEGER);
CREATE TABLE key_names (id INTEGER PRIMARY KEY, key STRING UNIQUE);
CREATE TABLE rule_results (key_id INTEGER PRIMARY KEY, value BLOB, signature INTEGER, built_at INTEGER, computed_at INTEGER, start REAL, end REAL, dependencies BLOB, FOREIGN KEY(key_id) REFERENCES key_names(id));
`
	_, err = db.ExecContext(ctx, schema)
	require.NoError(t, err)

	if iteration > 0 {
		_, err = db.ExecContext(ctx, "INSERT INTO info(id, version, client_version, iteration) VALUES (0, 17, 9, ?)", iteration)
		require.NoError(t, err)
	}

	for _, r := range rows {
		_, err = db.ExecContext(ctx, "INSERT INTO key_names(id, key) VALUES (?, ?)", r.keyID, r.key)
		require.NoError(t, err)

		var value [1]byte
		_, err = db.ExecContext(ctx,
			"INSERT INTO rule_results(key_id, value, signature, built_at, computed_at, start, end, dependencies) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)",
			r.keyID, value[:], r.signature, r.builtAt, r.builtAt, r.start, r.start+0.01)
		require.NoError(t, err)
	}

	require.NoError(t, db.Close())

	return path
}

