package enrichment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/sessions"
)

// supportedSidecarSchema matches sessions.SidecarSchemaVersion; higher values
// are left on disk for a newer binary to consume.
const supportedSidecarSchema = sessions.SidecarSchemaVersion

// expectedAncestor must appear in a sidecar's PeerAncestry list to qualify.
const expectedAncestor = "xcodebuild"

// SidecarStats is the merged per-invocation cache-stat view the enricher
// copies into analytics.InvocationRunStats.
type SidecarStats struct {
	Hits, Misses, KVHits, KVMisses int64
	Uploads                        int64
	UploadBytes, DownloadBytes     int64
	KVUploadBytes                  int64
	BlobStats                      *blobstats.Snapshot
}

// SidecarReader resolves sidecars left by the proxy that overlap a manifest
// group's time window.
type SidecarReader interface {
	Lookup(group ManifestEntryGroup) (SidecarStats, []string, bool)
}

type sidecarIndex struct {
	dir    string
	logger log.Logger
}

// NewSidecarIndex returns a SidecarReader backed by dir. A reader fresh-reads
// per Lookup; no caching.
func NewSidecarIndex(dir string, logger log.Logger) SidecarReader {
	return &sidecarIndex{dir: dir, logger: logger}
}

func (i *sidecarIndex) Lookup(group ManifestEntryGroup) (SidecarStats, []string, bool) {
	logger := logOr(i.logger)

	if i.dir == "" {
		logger.Debugf("Sidecar index skipped: empty dir")

		return SidecarStats{}, nil, false
	}

	entries, err := os.ReadDir(i.dir)
	if err != nil {
		logger.Debugf("Sidecar index read %s: %s", i.dir, err)

		return SidecarStats{}, nil, false
	}

	groupStart := group.Start()
	groupStop := group.Stop()

	var (
		merged         SidecarStats
		versionSkipped bool
		bestBlobTotal  int64
	)
	consumedPaths := make([]string, 0, len(entries))

	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		path := filepath.Join(i.dir, de.Name())

		body, err := os.ReadFile(path)
		if err != nil {
			logger.Debugf("Sidecar read %s: %s", path, err)

			continue
		}

		var s sessions.Sidecar
		if err := json.Unmarshal(body, &s); err != nil {
			logger.Debugf("Sidecar unmarshal %s: %s", path, err)

			continue
		}

		if s.SchemaVersion > supportedSidecarSchema {
			if !versionSkipped {
				logger.Debugf("Sidecar %s schema_version=%d > %d, leaving on disk", path, s.SchemaVersion, supportedSidecarSchema)
				versionSkipped = true
			}

			continue
		}

		if !ancestryMatches(s.PeerAncestry) {
			logger.Debugf("Sidecar %s ancestry rejected: %v", path, s.PeerAncestry)

			continue
		}

		if !overlaps(s.AcceptedAt, s.ClosedAt, groupStart, groupStop) {
			continue
		}

		if blob := s.BlobStats; blob != nil {
			total := blob.Download.OpCount + blob.Download.MissCount + blob.Upload.OpCount
			if merged.BlobStats == nil || total > bestBlobTotal {
				merged.BlobStats = blob
				bestBlobTotal = total
			}
		}

		consumedPaths = append(consumedPaths, path)
	}

	if len(consumedPaths) == 0 {
		return SidecarStats{}, nil, false
	}

	return merged, consumedPaths, true
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

func ancestryMatches(ancestry []string) bool {
	for _, a := range ancestry {
		if a == expectedAncestor {
			return true
		}
	}

	return false
}

// overlaps uses the half-open window [AcceptedAt, ClosedAt) ∩ [groupStart, groupStop).
// Any zero timestamp disqualifies the sidecar.
func overlaps(accepted, closed, groupStart, groupStop time.Time) bool {
	if accepted.IsZero() || closed.IsZero() || groupStart.IsZero() || groupStop.IsZero() {
		return false
	}

	return accepted.Before(groupStop) && groupStart.Before(closed)
}
