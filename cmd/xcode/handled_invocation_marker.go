package xcode

import (
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// handledMarkerMaxAge bounds how long an orphaned marker can survive before
// the proxy startup sweep removes it. Covers wrapper crashes between marker
// write and proxy F1 fire.
const handledMarkerMaxAge = 24 * time.Hour

// writeHandledMarker records that the wrapper already PUT a rich payload for
// invocationID; the proxy's slim emit checks this before doing its own PUT so
// the rich row survives last-write-wins.
//
// Best-effort: any failure only downgrades to the pre-fix behaviour (slim PUT
// overwrites), so we log at debug/warn and continue.
func writeHandledMarker(logger log.Logger, invocationID string) {
	if invocationID == "" {
		return
	}

	p, err := paths.Default()
	if err != nil {
		logger.Debugf("Handled-invocation marker skipped, cannot resolve paths: %v", err)

		return
	}

	dir := p.XcelerateHandledInvocationDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warnf("Handled-invocation marker skipped, mkdir failed: %v", err)

		return
	}

	file := p.XcelerateHandledInvocationFile(invocationID)
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec
	if err != nil {
		logger.Warnf("Handled-invocation marker skipped, open failed: %v", err)

		return
	}
	_ = f.Close()
}

// handledMarkerExists returns true when the wrapper already recorded a rich
// PUT for invocationID. Errors resolving paths silently fall back to false —
// slim emit proceeds as before.
func handledMarkerExists(invocationID string) bool {
	if invocationID == "" {
		return false
	}

	p, err := paths.Default()
	if err != nil {
		return false
	}

	_, err = os.Stat(p.XcelerateHandledInvocationFile(invocationID))

	return err == nil
}

// removeHandledMarker deletes the marker after the proxy observes it. Keeps
// the state dir from accumulating one file per invocation.
func removeHandledMarker(invocationID string) {
	if invocationID == "" {
		return
	}

	p, err := paths.Default()
	if err != nil {
		return
	}

	_ = os.Remove(p.XcelerateHandledInvocationFile(invocationID))
}

// pruneHandledMarkers removes marker files older than maxAge. Handles
// wrapper-crashed-before-slim-fire cases so the state dir stays bounded.
func pruneHandledMarkers(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}

		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
