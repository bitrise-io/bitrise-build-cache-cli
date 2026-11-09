package proxy

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless"
)

// BlobStatsSource exposes the proxy's global blobstats collector as the
// windowed source the wrapperless Allocator expects. Snapshot + Reset yields
// the delta since the previous bucket emit; callers (currently the pipeline's
// Allocator) own the cadence.
func (p *Proxy) BlobStatsSource() wrapperless.BlobStatsSource {
	if p == nil || p.sessionState == nil {
		return nil
	}

	return &protocolCollectorBlobStatsSource{c: p.sessionState.blobStats}
}

type protocolCollectorBlobStatsSource struct {
	c *blobstats.ProtocolCollector
}

func (s *protocolCollectorBlobStatsSource) Snapshot() *blobstats.Snapshot {
	if s == nil || s.c == nil {
		return nil
	}

	snap := s.c.Snapshot()

	return &snap
}

func (s *protocolCollectorBlobStatsSource) Reset() {
	if s == nil || s.c == nil {
		return
	}

	s.c.Reset()
}
