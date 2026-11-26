package buildanalyzer

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver registration
)

// RuleResult is one joined row from `key_names` × `rule_results`.
//
// Field semantics discovered by inspecting a real Xcode 26 build.db on disk
// (see docs/xcode-build-analyzer.md v2 notes):
//   - Key is the raw llbuild rule-key, which for commands is the manifest
//     command name prefixed with a one-byte rule-kind tag — `C` for a
//     Command rule, `N` for a Node (file / virtual node) rule. The reader
//     keeps the tag so callers decide their own matching strategy.
//   - Iteration is the rule's `built_at` iteration counter — a small integer
//     that equals `info.iteration` on the build that computed the row and
//     stays stuck at an older value for rules carried over unchanged. The
//     classifier uses this for the ReasonSkippedUpToDate / ReasonRanThisBuild
//     split.
//   - BuiltAtStart is the `start` REAL column (mach-absolute-time-based
//     seconds since boot, NOT CF absolute time; not comparable to
//     xcactivitylog timestamps). Kept as raw seconds for callers that need
//     per-rule ordering within a single build.
//   - Signature is the rule's llbuild signature, an 8-byte opaque hash. The
//     column is stored as SQLite INTEGER but the underlying bytes are the raw
//     signature; we marshal to big-endian bytes so a hex string in the Task
//     JSON renders consistently.
//   - Value is the opaque llbuild rule-value blob; v2 does not interpret it.
type RuleResult struct {
	Key          string
	Iteration    int64
	BuiltAtStart float64
	Signature    []byte
	Value        []byte
}

// Snapshot is the fully-joined build.db contents. Rules is keyed by raw
// rule-key (Lookup is via the `C` + ManifestKey convention for Command rules,
// with a fallback to the raw Task.Key). Iteration carries `info.iteration` —
// the current build's monotonically-growing counter, used by Classify to split
// ReasonRanThisBuild vs. ReasonSkippedUpToDate on the Xcode 26 schema where
// `rule_results.built_at` turned out to be an iteration counter, not a
// timestamp (see docs/xcode-build-analyzer.md v2 notes).
//
// Deps is the per-rule upstream adjacency — for each rule key, the list of
// rule keys it depends on. Xcode 26 does NOT ship a separate
// `rule_dependencies` table despite what llbuild's public docs suggest:
// the edges live inside a BLOB column (`rule_results.dependencies`) encoded
// as a packed array of little-endian int64 key_ids. The reader decodes that
// blob and resolves each int back to its key_names string so callers see a
// pure `string -> []string` adjacency. Empty / absent BLOBs result in no
// Deps entry for that rule.
//
// The struct shape is a small deviation from the task spec's `map[string]RuleResult`
// — the iteration counter has to ride along somewhere and a sentinel key in a
// bare map would be uglier than naming the field.
type Snapshot struct {
	Rules     map[string]RuleResult
	Deps      map[string][]string
	Iteration int64
	// PriorRules carries the rule_results map from the immediately-earlier
	// snapshot, when one is available (populated by ReadSnapshotPair /
	// PickSnapshotForLog). Classify uses it to tell new-task from
	// signature-changed on top of the single-snapshot buckets. Nil when no
	// prior snapshot was found — the classifier preserves its pre-B.2
	// behavior in that case.
	PriorRules map[string]RuleResult
}

// LocateBuildDB picks the `build.db` that goes with the most recent manifest.
//
// Xcode 26 lays the DB at the XCBuildData directory root — one DB covers every
// `.xcbuilddata` sibling inside it — so we return
// `<derivedData>/Build/Intermediates.noindex/XCBuildData/build.db` directly.
// Empty + nil error signals "no DB to read against" (clean project or wrong
// DerivedData path) so the CLI can soft-fall-back to v1 output.
func LocateBuildDB(derivedData string) (string, error) {
	path := filepath.Join(derivedData, "Build", "Intermediates.noindex", "XCBuildData", "build.db")

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("stat %s: %w", path, err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, expected file", path)
	}

	return path, nil
}

// ReadBuildDB opens path (read-only, no journal) and returns a Snapshot joining
// every `rule_results` row to its `key_names.key`. The read is one SELECT; the
// DB is tiny (hundreds of KB) and post-mortem, so we hold no handles after
// return.
func ReadBuildDB(path string) (Snapshot, error) {
	// `mode=ro` + `immutable=1` lets modernc.org/sqlite skip WAL / journal
	// touches on an Xcode-owned DB we have no business mutating; `cache=shared`
	// is irrelevant here (single-use handle) but makes the DSN match the
	// idiomatic read-only form.
	dsn := "file:" + path + "?mode=ro&immutable=1"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	iteration, err := readIteration(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}

	rules, deps, err := readRules(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{Rules: rules, Deps: deps, Iteration: iteration}, nil
}

// readIteration returns the current/latest build iteration counter from the
// `info` row. The classifier compares this against each rule's `built_at` to
// tell ReasonRanThisBuild from ReasonSkippedUpToDate. On the empty-table case
// (no `info` row — not expected but survivable) we fall back to 0; a cold
// snapshot where every rule's built_at < iteration is still correctly classed
// as ReasonSkippedUpToDate downstream.
func readIteration(ctx context.Context, db *sql.DB) (int64, error) {
	row := db.QueryRowContext(ctx, "SELECT iteration FROM info ORDER BY id DESC LIMIT 1")

	var iteration int64
	switch err := row.Scan(&iteration); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("read info.iteration: %w", err)
	}

	return iteration, nil
}

// readRules returns (rules by key, adjacency by key).
//
// Two-pass: first pass loads every row into a slice keyed by `key_id` so the
// second pass can resolve each `dependencies` BLOB's int64 key_ids back into
// string keys. The BLOB is a packed little-endian int64 array — no header,
// length = len(blob)/8.
//
// If a BLOB references a key_id that wasn't in `key_names` (shouldn't happen
// on a sane Xcode write) that entry is silently dropped: the attribution walk
// deals in Command rules, and a missing node in the adjacency is already how
// "no upstream" reads.
func readRules(ctx context.Context, db *sql.DB) (map[string]RuleResult, map[string][]string, error) {
	const q = `
SELECT r.key_id, k.key, r.built_at, r.start, r.signature, r.value, r.dependencies
FROM rule_results r
JOIN key_names k ON k.id = r.key_id
`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, nil, fmt.Errorf("query rule_results: %w", err)
	}
	defer rows.Close()

	type pending struct {
		key     string
		depsRaw []byte
	}

	rules := make(map[string]RuleResult, 1024)
	keyByID := make(map[int64]string, 1024)
	pendings := make([]pending, 0, 1024)

	for rows.Next() {
		var (
			keyID     int64
			key       string
			builtAt   int64
			start     sql.NullFloat64
			signature sql.NullInt64
			value     []byte
			depsRaw   []byte
		)

		if err := rows.Scan(&keyID, &key, &builtAt, &start, &signature, &value, &depsRaw); err != nil {
			return nil, nil, fmt.Errorf("scan rule_results row: %w", err)
		}

		rules[key] = RuleResult{
			Key:          key,
			Iteration:    builtAt,
			BuiltAtStart: start.Float64,
			Signature:    signatureBytes(signature),
			Value:        value,
		}
		keyByID[keyID] = key
		if len(depsRaw) > 0 {
			pendings = append(pendings, pending{key: key, depsRaw: depsRaw})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate rule_results: %w", err)
	}

	deps := make(map[string][]string, len(pendings))
	for _, p := range pendings {
		resolved := resolveDepBlob(p.depsRaw, keyByID)
		if len(resolved) > 0 {
			deps[p.key] = resolved
		}
	}

	return rules, deps, nil
}

// resolveDepBlob decodes a packed little-endian int64 array where each slot
// holds `(key_id << 2) | tag`. The low 2 bits carry an llbuild dep-kind tag
// (observed values: direct / order-only / signal-only, exact semantics
// undocumented); upper bits are the key_names.id. Shifting without the mask
// loses 79% of real edges to a false out-of-range filter — verified against
// Kickstarter + MinimalApp DBs where every post-shift id landed in
// [1, max(key_names.id)]. A BLOB whose length is not a multiple of 8 is
// treated as malformed and dropped silently.
func resolveDepBlob(raw []byte, keyByID map[int64]string) []string {
	if len(raw)%8 != 0 {
		return nil
	}

	out := make([]string, 0, len(raw)/8)
	for i := 0; i+8 <= len(raw); i += 8 {
		// Upper 62 bits = key_id, lower 2 bits = dep-kind tag (dropped).
		packed := int64(binary.LittleEndian.Uint64(raw[i : i+8])) //nolint:gosec // raw bit pattern, no overflow risk
		id := packed >> 2
		if k, ok := keyByID[id]; ok {
			out = append(out, k)
		}
	}

	return out
}

// signatureBytes serializes the SQLite INTEGER signature to an 8-byte
// big-endian blob. Big-endian keeps the hex rendering stable across hosts —
// the raw bytes land in the Task JSON as a hex string via encoding/hex.
func signatureBytes(signature sql.NullInt64) []byte {
	if !signature.Valid {
		return nil
	}

	// int64 → uint64 is a reinterpret of the two's-complement bit pattern — we
	// want the raw 8 bytes llbuild stashed, not the arithmetic value, so the
	// overflow warning is a false positive here.
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(signature.Int64)) //nolint:gosec // raw bit pattern, no overflow risk

	// Strip leading zeros so small signatures (test fixtures) don't render as
	// "0000000000000539" — the raw rule-signature can occupy fewer bytes and
	// the hex shape is still unique.
	start := 0
	for start < 7 && b[start] == 0 {
		start++
	}

	return append([]byte(nil), b[start:]...)
}
