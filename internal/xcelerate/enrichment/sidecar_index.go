package enrichment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

// supportedSidecarSchema matches the proxy's SidecarSchemaVersion; higher
// values are left on disk for a newer binary to consume.
const supportedSidecarSchema = 1

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
func NewSidecarIndex(dir string, logger log.Logger) *sidecarIndex {
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
		consumedPaths  []string
		versionSkipped bool
		bestBlobTotal  int64
	)

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

		var s sessionSidecar
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

		merged.Hits += s.Stats.Hits
		merged.Misses += s.Stats.Misses
		merged.KVHits += s.Stats.KVHits
		merged.KVMisses += s.Stats.KVMisses
		merged.Uploads += s.Stats.Uploads
		merged.UploadBytes += s.Stats.UploadBytes
		merged.DownloadBytes += s.Stats.DownloadBytes
		merged.KVUploadBytes += s.Stats.KVUploadBytes

		if blob := s.Stats.BlobStats; blob != nil {
			total := s.Stats.Hits + s.Stats.Misses
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

// sessionSidecar mirrors the proxy's SessionSidecar JSON for the subset the
// reader consumes.
type sessionSidecar struct {
	SchemaVersion int               `json:"schema_version"`
	PeerAncestry  []string          `json:"peer_ancestry"`
	AcceptedAt    time.Time         `json:"accepted_at"`
	ClosedAt      time.Time         `json:"closed_at"`
	Stats         sidecarStatsOnDisk `json:"stats"`
}

type sidecarStatsOnDisk struct {
	Hits          int64               `json:"hits"`
	Misses        int64               `json:"misses"`
	KVHits        int64               `json:"kv_hits"`
	KVMisses      int64               `json:"kv_misses"`
	Uploads       int64               `json:"uploads"`
	UploadBytes   int64               `json:"upload_bytes"`
	DownloadBytes int64               `json:"download_bytes"`
	KVUploadBytes int64               `json:"kv_upload_bytes"`
	BlobStats     *blobstats.Snapshot `json:"blob_stats,omitempty"`
}

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
