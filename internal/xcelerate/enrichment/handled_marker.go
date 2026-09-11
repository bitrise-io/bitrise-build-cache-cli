package enrichment

import (
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// HandledMarkerMaxAge bounds how long an orphaned marker can survive before the
// proxy startup sweep removes it. Covers wrapper crashes between marker write
// and the consumer (slim emit or enrichment watcher) firing.
const HandledMarkerMaxAge = 24 * time.Hour

// WriteMarker claims invocationID for the wrapper so slim emit and the enrichment
// watcher skip their own PUT and preserve the rich row from last-write-wins.
// Must precede the wrapper's PUT: both consumers check-then-PUT, so a later claim
// leaves them a window to clobber it. Best-effort — failures downgrade to the
// pre-fix behaviour and are logged only.
func WriteMarker(logger log.Logger, invocationID string) {
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

// RemoveMarker releases the claim when the wrapper's PUT failed, so the
// slim/enrichment fallbacks may write their row instead.
func RemoveMarker(logger log.Logger, invocationID string) {
	if invocationID == "" {
		return
	}

	p, err := paths.Default()
	if err != nil {
		logger.Debugf("Handled-invocation marker release skipped, cannot resolve paths: %v", err)

		return
	}

	if err := os.Remove(p.XcelerateHandledInvocationFile(invocationID)); err != nil && !os.IsNotExist(err) {
		logger.Warnf("Handled-invocation marker release failed: %v", err)
	}
}

func MarkerExists(invocationID string) bool {
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

func PruneStale(dir string, maxAge time.Duration) {
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
