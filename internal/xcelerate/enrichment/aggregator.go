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

// ProxyIDGroup is the single-ID pipeline's aggregated per-InvocationID bucket.
// Keyed by GroupKey(sidecar) — the proxy-derived InvocationID. DarkBuild
// sidecars fold INTO the matching MainBuild bucket at append time; every
// other RowType gets its own bucket.
type ProxyIDGroup struct {
	InvocationID    string
	Primary         RowType
	Blob            *blobstats.Snapshot
	Start           time.Time
	Stop            time.Time
	SidecarPaths    []string
	DerivedDataPath string
	BuildRequestHex string
	// Terminal is true when ANY sidecar in the group is Terminal=true. The
	// scan loop uses this to decide whether the group is eligible for an
	// immediate PUT or must wait on the plist / crash-fallback timer.
	Terminal bool
}

// loadedSidecar pairs a parsed sidecar with its on-disk path + classification.
// Internal to the two-pass aggregator so the Dark→Main fold is independent
// of readdir order.
type loadedSidecar struct {
	path string
	s    sessions.Sidecar
	rt   RowType
}

// GroupSidecarsByProxyID scans the sessions dir and buckets sidecars by
// proxy-derived InvocationID. DarkBuild sidecars (build + qos=default) fold
// into the MainBuild group that shares the same DerivedDataPath; a dark-only
// sidecar without a MainBuild peer keeps its own group so the enricher can
// still emit a row for it.
//
// Parallel to invocationAggregator — this is the single-ID pipeline's reader
// and does not share state with the time-overlap Lookup path. Returns nil on
// unreadable dir.
func GroupSidecarsByProxyID(dir string, logger log.Logger) map[string]*ProxyIDGroup {
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		logOr(logger).Debugf("ProxyID aggregator read %s: %s", dir, err)

		return nil
	}

	loads := collectLoadedSidecars(dir, entries, logger)
	mainBuildIDByDD := mapMainBuildIDsByDD(loads)

	groups := make(map[string]*ProxyIDGroup)
	for _, l := range loads {
		targetID := resolveTargetID(l, mainBuildIDByDD)
		appendIntoGroup(groups, targetID, l, logger)
	}

	return groups
}

func collectLoadedSidecars(dir string, entries []os.DirEntry, logger log.Logger) []loadedSidecar {
	loads := make([]loadedSidecar, 0, len(entries))
	var versionSkipped bool
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		path := filepath.Join(dir, de.Name())
		s, ok := loadSidecarForProxyAggregator(path, logger, &versionSkipped)
		if !ok {
			continue
		}

		if GroupKey(s) == "" {
			continue
		}

		loads = append(loads, loadedSidecar{path: path, s: s, rt: Classify(s.BuildRequestAction, s.BuildRequestQoS)})
	}

	return loads
}

func mapMainBuildIDsByDD(loads []loadedSidecar) map[string]string {
	out := make(map[string]string)
	for _, l := range loads {
		if l.rt == RowMainBuild && l.s.DerivedDataPath != "" {
			out[l.s.DerivedDataPath] = GroupKey(l.s)
		}
	}

	return out
}

func resolveTargetID(l loadedSidecar, mainBuildIDByDD map[string]string) string {
	targetID := GroupKey(l.s)
	if l.rt == RowDarkBuild && l.s.DerivedDataPath != "" {
		if mainID, exists := mainBuildIDByDD[l.s.DerivedDataPath]; exists {
			return mainID
		}
	}

	return targetID
}

func appendIntoGroup(groups map[string]*ProxyIDGroup, targetID string, l loadedSidecar, logger log.Logger) {
	g := groups[targetID]
	if g == nil {
		g = &ProxyIDGroup{
			InvocationID:    targetID,
			Primary:         l.rt,
			DerivedDataPath: l.s.DerivedDataPath,
			BuildRequestHex: l.s.BuildRequestHex,
		}
		groups[targetID] = g
	}

	if l.rt == RowMainBuild {
		g.Primary = RowMainBuild
	}

	g.Blob = mergeBlobs(g.Blob, l.s.BlobStats, logger)
	g.Start = earliestNonZero(g.Start, l.s.AcceptedAt, l.s.BuildStartedAt)

	if !l.s.ClosedAt.IsZero() && l.s.ClosedAt.After(g.Stop) {
		g.Stop = l.s.ClosedAt
	}
	if l.s.Terminal {
		g.Terminal = true
	}
	if g.DerivedDataPath == "" {
		g.DerivedDataPath = l.s.DerivedDataPath
	}
	if g.BuildRequestHex == "" {
		g.BuildRequestHex = l.s.BuildRequestHex
	}
	g.SidecarPaths = append(g.SidecarPaths, l.path)
}

func earliestNonZero(current time.Time, candidates ...time.Time) time.Time {
	best := current
	for _, t := range candidates {
		if t.IsZero() {
			continue
		}

		if best.IsZero() || t.Before(best) {
			best = t
		}
	}

	return best
}

// loadSidecarForProxyAggregator reads + schema-gates a sidecar for the
// proxy-ID aggregator. Mirrors loadSidecar but tolerates non-anchored
// ancestries (SWBBuildService-only) because the single-ID path trusts the
// WrapperSession anchor rather than the accept-list.
func loadSidecarForProxyAggregator(path string, logger log.Logger, versionSkipped *bool) (sessions.Sidecar, bool) {
	l := logOr(logger)

	body, err := os.ReadFile(path)
	if err != nil {
		l.Debugf("ProxyID aggregator read %s: %s", path, err)

		return sessions.Sidecar{}, false
	}

	var s sessions.Sidecar
	if err := json.Unmarshal(body, &s); err != nil {
		l.Debugf("ProxyID aggregator unmarshal %s: %s", path, err)

		return sessions.Sidecar{}, false
	}

	if s.SchemaVersion != supportedSidecarSchema {
		if !*versionSkipped {
			l.Debugf("ProxyID aggregator schema mismatch %s=%d want %d", path, s.SchemaVersion, supportedSidecarSchema)
			*versionSkipped = true
		}

		return sessions.Sidecar{}, false
	}

	return s, true
}
