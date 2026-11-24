//go:build unit

package buildanalyzer

import (
	"context"
	"database/sql"
	"encoding/binary"
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

// TestReadBuildDB_ParsesDependenciesBLOB asserts the little-endian packed
// int64 decode of the `rule_results.dependencies` column: a Command rule
// pointing at a Node, which itself points at an upstream Command, should
// surface as two Snapshot.Deps rows the attribution walker can traverse.
func TestReadBuildDB_ParsesDependenciesBLOB(t *testing.T) {
	// Command 1 reads Node 3. Node 3 is produced by Command 2. Command 2
	// has no deps. The walker's `C -> N -> C` hop needs both halves.
	path := prepareTempDB(t, []insertRow{
		{keyID: 1, key: "CP0:::CompileFoo", builtAt: 5, start: 1, signature: 111, depIDs: []int64{3}},
		{keyID: 2, key: "CP0:::EmitBar", builtAt: 5, start: 1, signature: 222},
		{keyID: 3, key: "N/tmp/Bar.o", builtAt: 5, start: 1, signature: 333, depIDs: []int64{2}},
	}, 5)

	snap, err := ReadBuildDB(path)
	require.NoError(t, err)
	require.NotNil(t, snap.Deps)

	require.ElementsMatch(t, []string{"N/tmp/Bar.o"}, snap.Deps["CP0:::CompileFoo"])
	require.ElementsMatch(t, []string{"CP0:::EmitBar"}, snap.Deps["N/tmp/Bar.o"])
	require.NotContains(t, snap.Deps, "CP0:::EmitBar", "rules with no deps BLOB are absent from the adjacency")
}

// TestReadBuildDB_DropsUnknownDepIDs covers the robustness path: a deps BLOB
// that references a key_id not present in `key_names` (should never happen
// but could survive a partial DB truncation) is dropped silently — the dep
// just doesn't surface.
func TestReadBuildDB_DropsUnknownDepIDs(t *testing.T) {
	path := prepareTempDB(t, []insertRow{
		{keyID: 1, key: "CA", builtAt: 1, start: 1, signature: 1, depIDs: []int64{999}},
	}, 1)

	snap, err := ReadBuildDB(path)
	require.NoError(t, err)
	// 999 isn't a known key, so the adjacency entry drops to empty →
	// filtered out of the map entirely.
	require.NotContains(t, snap.Deps, "CA")
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
// SQLite will treat as an INTEGER column. depIDs is the packed dep blob input
// — the helper encodes each int64 as little-endian.
type insertRow struct {
	keyID     int64
	key       string
	builtAt   int64
	start     float64
	signature int64
	depIDs    []int64
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

		var depsBlob interface{} // keep NULL semantics when depIDs is nil
		if len(r.depIDs) > 0 {
			raw := make([]byte, len(r.depIDs)*8)
			for i, id := range r.depIDs {
				binary.LittleEndian.PutUint64(raw[i*8:(i+1)*8], uint64(id)) //nolint:gosec // raw bit pattern
			}
			depsBlob = raw
		}

		_, err = db.ExecContext(ctx,
			"INSERT INTO rule_results(key_id, value, signature, built_at, computed_at, start, end, dependencies) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			r.keyID, value[:], r.signature, r.builtAt, r.builtAt, r.start, r.start+0.01, depsBlob)
		require.NoError(t, err)
	}

	require.NoError(t, db.Close())

	return path
}

