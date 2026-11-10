package proxy

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless"
)

// BlobStatsSource exposes the proxy's global blobstats collector as the
// windowed source the wrapperless Allocator expects. TakeSnapshot yields the
// delta since the previous bucket emit, snapshotting and resetting under one
// locked region so a concurrent RPC cannot lose bytes between the two calls.
func (p *Proxy) BlobStatsSource() wrapperless.BlobStatsSource {
	if p == nil || p.sessionState == nil {
		return nil
	}

	return &protocolCollectorBlobStatsSource{c: p.sessionState.blobStats}
}

type protocolCollectorBlobStatsSource struct {
	c *blobstats.ProtocolCollector
}

func (s *protocolCollectorBlobStatsSource) TakeSnapshot() *blobstats.Snapshot {
	if s == nil || s.c == nil {
		return nil
	}

	snap := s.c.TakeSnapshot()

	return &snap
}
