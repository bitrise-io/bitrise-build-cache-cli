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
//   - BuiltAt is the rule's `built_at` iteration counter, NOT a wall-clock
//     timestamp — on the Xcode 26 schema it's a small integer that equals
//     `info.iteration` on the build that computed the row and stays stuck at
//     an older value for rules carried over unchanged. The classifier uses
//     this for the ReasonSkippedUpToDate / ReasonRanThisBuild split. We still
//     expose it as a time.Time for the JSON shape promised by the task
//     spec — encoded as Unix(iteration, 0) so round-tripping the counter is
//     lossless.
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
	BuiltAt      time.Time
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
// The struct shape is a small deviation from the task spec's `map[string]RuleResult`
// — the iteration counter has to ride along somewhere and a sentinel key in a
// bare map would be uglier than naming the field.
type Snapshot struct {
	Rules     map[string]RuleResult
	Iteration int64
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

	rules, err := readRules(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{Rules: rules, Iteration: iteration}, nil
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

func readRules(ctx context.Context, db *sql.DB) (map[string]RuleResult, error) {
	const q = `
SELECT k.key, r.built_at, r.start, r.signature, r.value
FROM rule_results r
JOIN key_names k ON k.id = r.key_id
`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query rule_results: %w", err)
	}
	defer rows.Close()

	out := make(map[string]RuleResult, 1024)

	for rows.Next() {
		var (
			key       string
			builtAt   int64
			start     sql.NullFloat64
			signature sql.NullInt64
			value     []byte
		)

		if err := rows.Scan(&key, &builtAt, &start, &signature, &value); err != nil {
			return nil, fmt.Errorf("scan rule_results row: %w", err)
		}

		out[key] = RuleResult{
			Key:          key,
			BuiltAt:      iterationToTime(builtAt),
			BuiltAtStart: start.Float64,
			Signature:    signatureBytes(signature),
			Value:        value,
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rule_results: %w", err)
	}

	return out, nil
}

// iterationToTime encodes the rule's built_at iteration counter in a time.Time
// so the JSON field the task spec promised round-trips the integer losslessly.
// Classification uses the raw counter, not the time; this is purely a carrier.
// time.Time{} (zero) stays zero so downstream JSON omitempty behaves.
func iterationToTime(builtAt int64) time.Time {
	if builtAt == 0 {
		return time.Time{}
	}

	return time.Unix(builtAt, 0).UTC()
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
