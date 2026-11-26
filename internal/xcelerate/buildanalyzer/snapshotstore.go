package buildanalyzer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver registration
)

// EnvSnapshotRoot overrides DefaultSnapshotRoot when set — used by tests and
// opt-in operator overrides so multiple CLIs can share one snapshot history.
const EnvSnapshotRoot = "BITRISE_BUILD_ANALYZER_SNAPSHOT_ROOT"

// snapshotsRelative is the per-user snapshot root rooted at $HOME.
const snapshotsRelative = ".local/state/xcelerate/build-analyzer/snapshots"

// maxSnapshotsPerBuild caps the retained snapshots per build-hex directory.
// Older-than-tenth snapshots are pruned on every SnapshotBuildDB — the signal
// has a short shelf-life (one more Xcode build invalidates it) and the DB is
// tens of MB, so a tight cap trumps unbounded growth.
const maxSnapshotsPerBuild = 10

// SnapshotMeta is one snapshot's identity — enough to pick the right prior
// snapshot from a directory listing without re-opening the file.
type SnapshotMeta struct {
	Path      string
	BuildHex  string
	Iteration int64
	Timestamp time.Time
}

// DefaultSnapshotRoot returns the on-disk root for build-analyzer snapshots.
// The env override wins so operator / test harness can redirect the store
// without a flag flood on every consumer.
func DefaultSnapshotRoot() string {
	if v := os.Getenv(EnvSnapshotRoot); v != "" {
		return v
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to a tmpdir so the caller can still proceed — nothing in
		// the snapshot flow is fatal if the root resolves oddly.
		return filepath.Join(os.TempDir(), "bitrise-build-analyzer", "snapshots")
	}

	return filepath.Join(home, snapshotsRelative)
}

// SnapshotBuildDB copies the live build.db under derivedData into storeRoot,
// keyed by build-hex + iteration, and prunes older snapshots over the cap.
// Returns the written snapshot path.
//
// Locating logic mirrors the analyzer flow — build.db via LocateBuildDB,
// build-hex via the newest-mtime `*.xcbuilddata` sibling (same picker
// LocateManifest uses). Missing DB returns ("", nil) so the CLI can soft-fail
// the same way it does on analyze.
//
// Copy strategy: `VACUUM INTO` via modernc.org/sqlite. Verified on a real
// Kickstarter build.db (40 MB, WAL-mode) with the read-only+immutable DSN —
// iteration preserved, rule_results row count matches, snapshot ~8% smaller
// (37 MB) from the on-disk defrag VACUUM does. Compared to a raw file copy,
// VACUUM INTO:
//   - Produces a WAL-free, journal-free single file (the analyzer's
//     ReadBuildDB opens with `mode=ro&immutable=1` and expects no sidecars).
//   - Handles the hot-source case: if Xcode is mid-build, a plain file copy
//     races the writer; VACUUM INTO takes a read lock and serializes cleanly.
//   - Is atomic from the caller's side — SQLite builds the destination in a
//     temp file and renames.
//
// If VACUUM INTO ever fails on a future SQLite+WAL combo the CLI should surface
// the error rather than silently cut over; a `.backup` fallback is tracked but
// not implemented — the current signal says we don't need it.
func SnapshotBuildDB(derivedData, storeRoot string) (string, error) {
	dbPath, err := LocateBuildDB(derivedData)
	if err != nil {
		return "", err
	}

	if dbPath == "" {
		return "", nil
	}

	buildHex, err := locateBuildHex(derivedData)
	if err != nil {
		return "", err
	}

	if buildHex == "" {
		return "", fmt.Errorf("no *.xcbuilddata directory under %s", derivedData)
	}

	iteration, err := readIterationForSnapshot(dbPath)
	if err != nil {
		return "", err
	}

	targetDir := filepath.Join(storeRoot, buildHex)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create snapshot dir %s: %w", targetDir, err)
	}

	now := time.Now().UTC()
	// Zero-pad iteration so lexicographic sort within a filename prefix stays
	// monotonic; `snapshotRegex` strips the leading zeros on readback.
	snapName := fmt.Sprintf("%010d-%d.db", iteration, now.Unix())
	dst := filepath.Join(targetDir, snapName)

	if err := vacuumInto(dbPath, dst); err != nil {
		return "", err
	}

	if err := pruneSnapshots(targetDir); err != nil {
		return "", err
	}

	return dst, nil
}

// ListSnapshots returns the retained snapshots for buildHex sorted descending
// by iteration (newer first), then descending by timestamp as tiebreaker.
// Returns an empty slice when the directory does not exist.
func ListSnapshots(storeRoot, buildHex string) ([]SnapshotMeta, error) {
	dir := filepath.Join(storeRoot, buildHex)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read snapshot dir %s: %w", dir, err)
	}

	out := make([]SnapshotMeta, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		meta, ok := parseSnapshotName(e.Name())
		if !ok {
			continue
		}

		meta.Path = filepath.Join(dir, e.Name())
		meta.BuildHex = buildHex
		out = append(out, meta)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Iteration != out[j].Iteration {
			return out[i].Iteration > out[j].Iteration
		}

		return out[i].Timestamp.After(out[j].Timestamp)
	})

	return out, nil
}

// Private — snapshot storage internals.

// locateBuildHex returns the hex basename of the most-recently-modified
// `*.xcbuilddata` directory under derivedData/XCBuildData. Mirrors the picker
// LocateManifest uses so B.1 and the manifest join agree on which build we're
// looking at when multiple xcbuilddata siblings coexist.
func locateBuildHex(derivedData string) (string, error) {
	root := filepath.Join(derivedData, "Build", "Intermediates.noindex", "XCBuildData")

	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("read %s: %w", root, err)
	}

	type cand struct {
		hex   string
		mtime int64
	}

	cands := make([]cand, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || filepath.Ext(e.Name()) != ".xcbuilddata" {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		hex := strings.TrimSuffix(e.Name(), ".xcbuilddata")
		cands = append(cands, cand{hex: hex, mtime: info.ModTime().UnixNano()})
	}

	if len(cands) == 0 {
		return "", nil
	}

	sort.Slice(cands, func(i, j int) bool { return cands[i].mtime > cands[j].mtime })

	return cands[0].hex, nil
}

// readIterationForSnapshot opens the DB read-only and reads info.iteration.
// Separate from ReadBuildDB to avoid eagerly materialising every rule row just
// to name the snapshot file.
func readIterationForSnapshot(path string) (int64, error) {
	dsn := "file:" + path + "?mode=ro&immutable=1"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return readIteration(ctx, db)
}

// vacuumInto runs `VACUUM INTO '<dst>'` against src. The destination path is
// shell-quoted via sqlite's string literal escape (`”`), not Go's %q, so
// paths containing single-quotes don't trip the planner. Shouldn't ever happen
// in practice (storeRoot is under $HOME/.local/state) but it's cheap to be
// correct.
func vacuumInto(src, dst string) error {
	// If a previous attempt left a half-written file behind, SQLite refuses to
	// overwrite. Remove it first — the pending state is non-recoverable anyway.
	if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove stale snapshot %s: %w", dst, err)
	}

	dsn := "file:" + src + "?mode=ro&immutable=1"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// SQLite escapes a single quote inside a string literal by doubling it.
	// VACUUM INTO requires a literal path (parameter binding is not supported
	// by the SQLite grammar for this statement), so concatenation is the only
	// path forward — we escape ' to '' ourselves and dst is never user-text at
	// the request level anyway (it's always under storeRoot).
	escaped := strings.ReplaceAll(dst, "'", "''")
	stmt := "VACUUM INTO '" + escaped + "'" //nolint:gosec // VACUUM INTO can't bind parameters; dst is escaped above

	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("VACUUM INTO %s: %w", dst, err)
	}

	return nil
}

// pruneSnapshots deletes older snapshots so no build-hex dir keeps more than
// maxSnapshotsPerBuild entries. Sort order matches ListSnapshots (iteration
// desc, timestamp desc) — oldest get removed first.
func pruneSnapshots(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read snapshot dir %s: %w", dir, err)
	}

	metas := make([]SnapshotMeta, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		meta, ok := parseSnapshotName(e.Name())
		if !ok {
			continue
		}

		meta.Path = filepath.Join(dir, e.Name())
		metas = append(metas, meta)
	}

	if len(metas) <= maxSnapshotsPerBuild {
		return nil
	}

	sort.Slice(metas, func(i, j int) bool {
		if metas[i].Iteration != metas[j].Iteration {
			return metas[i].Iteration > metas[j].Iteration
		}

		return metas[i].Timestamp.After(metas[j].Timestamp)
	})

	for _, meta := range metas[maxSnapshotsPerBuild:] {
		if err := os.Remove(meta.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("prune %s: %w", meta.Path, err)
		}
	}

	return nil
}

// snapshotRegex parses the <iter>-<unix>.db filename shape.
var snapshotRegex = regexp.MustCompile(`^(\d+)-(\d+)\.db$`) //nolint:gochecknoglobals

// parseSnapshotName extracts (iteration, unix timestamp) from a snapshot
// filename. The sort routines use these fields; non-matching entries (e.g. a
// stray `.tmp` SQLite leaves on rare Linux OOM paths) are silently ignored.
func parseSnapshotName(name string) (SnapshotMeta, bool) {
	match := snapshotRegex.FindStringSubmatch(name)
	if match == nil {
		return SnapshotMeta{}, false
	}

	iter, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return SnapshotMeta{}, false
	}

	ts, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return SnapshotMeta{}, false
	}

	return SnapshotMeta{
		Iteration: iter,
		Timestamp: time.Unix(ts, 0).UTC(),
	}, true
}
