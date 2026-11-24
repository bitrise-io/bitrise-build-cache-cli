//go:build unit

package buildanalyzer

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// openSeedDB is a thin wrapper around sql.Open for tests that need to seed a
// fresh llbuild-shaped DB on disk. Factored so the DSN format sits in one spot
// and a future driver swap is a one-line change.
func openSeedDB(dsn string) (*sql.DB, error) {
	return sql.Open("sqlite", dsn)
}

// TestSnapshotBuildDB_RoundTripReadBack lays a tiny llbuild-shaped source DB
// under a fake DerivedData tree, snapshots it, and asserts ReadBuildDB on the
// snapshot returns the same iteration + rules.
func TestSnapshotBuildDB_RoundTripReadBack(t *testing.T) {
	derivedData, buildHex := prepareDerivedDataWithSrcDB(t, 7)
	storeRoot := t.TempDir()

	snapPath, err := SnapshotBuildDB(derivedData, storeRoot)
	require.NoError(t, err)
	require.FileExists(t, snapPath)

	// Snapshot sits under <storeRoot>/<buildHex>/<iter>-<ts>.db.
	assert.True(t, strings.HasPrefix(snapPath, filepath.Join(storeRoot, buildHex)+string(os.PathSeparator)),
		"snapshot path should sit under <storeRoot>/<buildHex>/: %s", snapPath)

	snap, err := ReadBuildDB(snapPath)
	require.NoError(t, err)
	assert.Equal(t, int64(7), snap.Iteration)
	assert.Len(t, snap.Rules, 2)

	foo, ok := snap.Rules["CP0:::CompileFoo"]
	require.True(t, ok)
	assert.Equal(t, int64(7), foo.Iteration)
}

// TestSnapshotBuildDB_SoftFailOnMissingDB covers the clean-project case —
// LocateBuildDB returns ("", nil), SnapshotBuildDB mirrors the contract.
func TestSnapshotBuildDB_SoftFailOnMissingDB(t *testing.T) {
	empty := t.TempDir()
	storeRoot := t.TempDir()

	snapPath, err := SnapshotBuildDB(empty, storeRoot)
	require.NoError(t, err)
	assert.Empty(t, snapPath)
}

// TestSnapshotBuildDB_ErrorsOnDBWithoutBuildHex writes a build.db directly
// but no `*.xcbuilddata` directory to associate it with — the caller should
// get a clear error so they don't accidentally scatter snapshots under an
// empty key.
func TestSnapshotBuildDB_ErrorsOnDBWithoutBuildHex(t *testing.T) {
	derivedData := t.TempDir()
	xcbuildDir := filepath.Join(derivedData, "Build", "Intermediates.noindex", "XCBuildData")
	require.NoError(t, os.MkdirAll(xcbuildDir, 0o755))

	dbPath := filepath.Join(xcbuildDir, "build.db")
	seedInMemoryLikeDB(t, dbPath, 1, []srcRow{{keyID: 1, key: "CA"}})

	_, err := SnapshotBuildDB(derivedData, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xcbuilddata")
}

// TestSnapshotBuildDB_PruneOldest creates 12 snapshots (via repeated calls
// with iteration bumps + a manual timestamp spread) and verifies pruneSnapshots
// kept the newest 10.
func TestSnapshotBuildDB_PruneOldest(t *testing.T) {
	derivedData, buildHex := prepareDerivedDataWithSrcDB(t, 1)
	storeRoot := t.TempDir()
	dir := filepath.Join(storeRoot, buildHex)

	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Synthesize 12 pre-existing snapshot files with ascending iteration (so
	// the sort order is deterministic) and valid DB contents so a follow-up
	// ReadBuildDB would still work.
	for i := 1; i <= 12; i++ {
		fname := fmt.Sprintf("%010d-%d.db", i, time.Now().Unix()-int64(12-i))
		seedInMemoryLikeDB(t, filepath.Join(dir, fname), int64(i), []srcRow{{keyID: 1, key: "CA"}})
	}

	// One snapshot call triggers pruning; it adds an entry for iteration 1
	// (the source DB's current iter) and then prunes down to 10.
	_, err := SnapshotBuildDB(derivedData, storeRoot)
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	// Count only .db files that match the snapshot regex — the test might
	// leave temp artifacts otherwise.
	snapshots := 0
	for _, e := range entries {
		if _, ok := parseSnapshotName(e.Name()); ok {
			snapshots++
		}
	}
	assert.Equal(t, maxSnapshotsPerBuild, snapshots, "prune cap enforced")
}

// TestListSnapshots_SortedByIterationDesc checks the returned slice is in the
// order downstream callers (B.2 pick) expect: newest iteration first.
func TestListSnapshots_SortedByIterationDesc(t *testing.T) {
	storeRoot := t.TempDir()
	hex := "d3b07384d113edec49eaa6238ad5ff00"
	dir := filepath.Join(storeRoot, hex)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	for _, iter := range []int{3, 1, 5, 2} {
		fname := fmt.Sprintf("%010d-%d.db", iter, time.Now().Unix()-int64(iter))
		seedInMemoryLikeDB(t, filepath.Join(dir, fname), int64(iter), []srcRow{{keyID: 1, key: "CA"}})
	}

	list, err := ListSnapshots(storeRoot, hex)
	require.NoError(t, err)
	require.Len(t, list, 4)
	assert.Equal(t, int64(5), list[0].Iteration)
	assert.Equal(t, int64(3), list[1].Iteration)
	assert.Equal(t, int64(2), list[2].Iteration)
	assert.Equal(t, int64(1), list[3].Iteration)
}

// TestListSnapshots_MissingDirReturnsEmpty covers the fresh-storeRoot case.
func TestListSnapshots_MissingDirReturnsEmpty(t *testing.T) {
	list, err := ListSnapshots(t.TempDir(), "does-not-exist")
	require.NoError(t, err)
	assert.Empty(t, list)
}

// TestDefaultSnapshotRoot_HonorsEnvOverride verifies the env var escape hatch.
func TestDefaultSnapshotRoot_HonorsEnvOverride(t *testing.T) {
	t.Setenv(EnvSnapshotRoot, "/custom/snap-root")
	assert.Equal(t, "/custom/snap-root", DefaultSnapshotRoot())
}

// Private — test helpers.

// srcRow is a slimmed-down insertRow (ruledb_test.go) for seeding tiny DBs
// where the dependency BLOB side isn't exercised.
type srcRow struct {
	keyID int64
	key   string
}

// prepareDerivedDataWithSrcDB lays a DerivedData tree holding one build.db +
// one `<hex>.xcbuilddata` sibling. Returns the derivedData root and the hex.
func prepareDerivedDataWithSrcDB(t *testing.T, iteration int64) (string, string) {
	t.Helper()

	derivedData := t.TempDir()
	xcbuildDir := filepath.Join(derivedData, "Build", "Intermediates.noindex", "XCBuildData")
	require.NoError(t, os.MkdirAll(xcbuildDir, 0o755))

	hex := "d3b07384d113edec49eaa6238ad5ff00"
	require.NoError(t, os.MkdirAll(filepath.Join(xcbuildDir, hex+".xcbuilddata"), 0o755))

	dbPath := filepath.Join(xcbuildDir, "build.db")
	seedInMemoryLikeDB(t, dbPath, iteration, []srcRow{
		{keyID: 1, key: "CP0:::CompileFoo"},
		{keyID: 2, key: "CP0:::LinkFoo"},
	})

	return derivedData, hex
}

// seedInMemoryLikeDB writes a tiny llbuild-schema DB at path with the given
// iteration and rule rows. Separate from ruledb_test.go's prepareTempDB so
// the helper can be aimed at an explicit path (needed for the DerivedData
// layout dance here).
func seedInMemoryLikeDB(t *testing.T, path string, iteration int64, rows []srcRow) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	// Use the exact same schema prepareTempDB writes — ReadBuildDB has to be
	// able to read these DBs back.
	writeLlbuildFixture(t, path, iteration, rows)
}

// writeLlbuildFixture is factored from ruledb_test.go's prepareTempDB so this
// file can emit DBs at caller-chosen paths. The schema matches Xcode 26's
// shape closely enough for ReadBuildDB.
func writeLlbuildFixture(t *testing.T, path string, iteration int64, rows []srcRow) {
	t.Helper()

	dsn := "file:" + path
	db, err := openSeedDB(dsn)
	require.NoError(t, err)
	defer db.Close()

	const schema = `
CREATE TABLE info (id INTEGER PRIMARY KEY, version INTEGER, client_version INTEGER, iteration INTEGER);
CREATE TABLE key_names (id INTEGER PRIMARY KEY, key STRING UNIQUE);
CREATE TABLE rule_results (key_id INTEGER PRIMARY KEY, value BLOB, signature INTEGER, built_at INTEGER, computed_at INTEGER, start REAL, end REAL, dependencies BLOB, FOREIGN KEY(key_id) REFERENCES key_names(id));
`
	_, err = db.Exec(schema)
	require.NoError(t, err)

	if iteration > 0 {
		_, err = db.Exec("INSERT INTO info(id, version, client_version, iteration) VALUES (0, 17, 9, ?)", iteration)
		require.NoError(t, err)
	}

	for _, r := range rows {
		_, err = db.Exec("INSERT INTO key_names(id, key) VALUES (?, ?)", r.keyID, r.key)
		require.NoError(t, err)

		_, err = db.Exec(
			"INSERT INTO rule_results(key_id, value, signature, built_at, computed_at, start, end, dependencies) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			r.keyID, []byte{0}, int64(42), iteration, iteration, 0.0, 0.01, nil)
		require.NoError(t, err)
	}
}
