package proxy

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// derivedDataLookupLimit caps how long the DD-path resolver may spend reading
// cmdline + cwd across the ancestry chain. Peer walks share a tight budget
// because a stuck ps/libproc call delays every subsequent TagConn.
const derivedDataLookupLimit = 500 * time.Millisecond

// peerDerivedDataPath walks the entries list for an "xcodebuild" ancestor and
// returns its parsed -derivedDataPath argument resolved against the process's
// cwd. Empty when no anchor is present, no flag was supplied, or the lookup
// could not resolve process metadata (dead PID, insufficient privilege).
func peerDerivedDataPath(entries []AncestryEntry) string {
	ctx, cancel := context.WithTimeout(context.Background(), derivedDataLookupLimit)
	defer cancel()

	return peerDerivedDataPathCtx(ctx, entries, resolveCmdlineAndCwd)
}

// peerDerivedDataPathCtx is the testable core: injectable process-metadata
// resolver, no gopsutil import at call sites.
func peerDerivedDataPathCtx(ctx context.Context, entries []AncestryEntry, resolve processMetaResolver) string {
	for _, e := range entries {
		if e.Name != "xcodebuild" {
			continue
		}

		args, cwd, err := resolve(ctx, e.PID)
		if err != nil || len(args) == 0 {
			continue
		}

		raw := extractDerivedDataFlag(args)
		if raw == "" {
			continue
		}

		if filepath.IsAbs(raw) {
			return filepath.Clean(raw)
		}

		if cwd == "" {
			return raw
		}

		return filepath.Clean(filepath.Join(cwd, raw))
	}

	return ""
}

// extractDerivedDataFlag handles both "-derivedDataPath" "<path>" and
// "-derivedDataPath=<path>" forms. Short option variants are not recognised —
// xcodebuild does not accept them.
func extractDerivedDataFlag(args []string) string {
	const flag = "-derivedDataPath"

	for i, a := range args {
		switch {
		case a == flag:
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, flag+"="):
			return strings.TrimPrefix(a, flag+"=")
		}
	}

	return ""
}

// processMetaResolver is the (cmdline, cwd) oracle; swap-in for tests.
type processMetaResolver func(ctx context.Context, pid int) (args []string, cwd string, err error)

func resolveCmdlineAndCwd(ctx context.Context, pid int) ([]string, string, error) {
	//nolint:gosec // PIDs fit in int32.
	proc, err := process.NewProcessWithContext(ctx, int32(pid))
	if err != nil {
		return nil, "", err //nolint:wrapcheck // caller treats as "unresolvable PID"
	}

	args, err := proc.CmdlineSliceWithContext(ctx)
	if err != nil {
		return nil, "", err //nolint:wrapcheck // caller treats as "unresolvable PID"
	}

	// Cwd may fail without privilege on darwin; empty cwd lets the caller fall
	// back to the raw flag value.
	cwd, _ := proc.CwdWithContext(ctx)

	return args, cwd, nil
}
