package blobstats

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

// ToProto returns nil for an empty session, so the wrapper sends no all-zero payload.
func ToProto(snapshot Snapshot) *session.CacheBlobStats {
	if snapshot.IsEmpty() {
		return nil
	}

	return &session.CacheBlobStats{ //nolint:exhaustruct // proto internals
		SchemaVersion: int32(snapshot.SchemaVersion), //nolint:gosec // small constant
		Upload:        directionToProto(snapshot.Upload),
		Download:      directionToProto(snapshot.Download),
		Cas:           protocolToProto(snapshot.CAS),
		Kv:            protocolToProto(snapshot.KV),
	}
}

func FromProto(pb *session.CacheBlobStats) *Snapshot {
	if pb == nil {
		return nil
	}

	return &Snapshot{
		SchemaVersion: int(pb.GetSchemaVersion()),
		Upload:        directionFromProto(pb.GetUpload()),
		Download:      directionFromProto(pb.GetDownload()),
		CAS:           protocolFromProto(pb.GetCas()),
		KV:            protocolFromProto(pb.GetKv()),
	}
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

func protocolToProto(p *ProtocolSnapshot) *session.ProtocolBlobStats {
	if p == nil {
		return nil
	}

	return &session.ProtocolBlobStats{ //nolint:exhaustruct // proto internals
		Upload:   directionToProto(p.Upload),
		Download: directionToProto(p.Download),
	}
}

func protocolFromProto(pb *session.ProtocolBlobStats) *ProtocolSnapshot {
	if pb == nil {
		return nil
	}

	return &ProtocolSnapshot{
		Upload:   directionFromProto(pb.GetUpload()),
		Download: directionFromProto(pb.GetDownload()),
	}
}

func directionToProto(d DirectionSnapshot) *session.BlobStats {
	return &session.BlobStats{ //nolint:exhaustruct // proto internals
		OpCount:                  d.OpCount,
		ErrorCount:               d.ErrorCount,
		MissCount:                d.MissCount,
		SkippedAlreadySavedCount: d.SkippedAlreadySavedCount,
		BytesTotal:               d.BytesTotal,
		LatencyMs:                histogramToProto(d.LatencyMs),
		SizeBytes:                histogramToProto(d.SizeBytes),
		Throughput: &session.ThroughputStats{ //nolint:exhaustruct // proto internals
			Histogram:        histogramToProto(d.Throughput.Histogram),
			P10BytesPerSec:   d.Throughput.P10BytesPerSec,
			P50BytesPerSec:   d.Throughput.P50BytesPerSec,
			P90BytesPerSec:   d.Throughput.P90BytesPerSec,
			MinBlobBytes:     d.Throughput.MinBlobBytes,
			ExcludedSmallOps: d.Throughput.ExcludedSmallOps,
		},
	}
}

func histogramToProto(h HistogramSnapshot) *session.Histogram {
	return &session.Histogram{ //nolint:exhaustruct // proto internals
		Boundaries: h.Boundaries,
		Counts:     h.Counts,
		Count:      h.Count,
		Sum:        h.Sum,
		Min:        h.Min,
		Max:        h.Max,
	}
}

func directionFromProto(pb *session.BlobStats) DirectionSnapshot {
	return DirectionSnapshot{
		OpCount:                  pb.GetOpCount(),
		ErrorCount:               pb.GetErrorCount(),
		MissCount:                pb.GetMissCount(),
		SkippedAlreadySavedCount: pb.GetSkippedAlreadySavedCount(),
		BytesTotal:               pb.GetBytesTotal(),
		LatencyMs:                histogramFromProto(pb.GetLatencyMs()),
		SizeBytes:                histogramFromProto(pb.GetSizeBytes()),
		Throughput: ThroughputSnapshot{
			Histogram:        histogramFromProto(pb.GetThroughput().GetHistogram()),
			P10BytesPerSec:   pb.GetThroughput().GetP10BytesPerSec(),
			P50BytesPerSec:   pb.GetThroughput().GetP50BytesPerSec(),
			P90BytesPerSec:   pb.GetThroughput().GetP90BytesPerSec(),
			MinBlobBytes:     pb.GetThroughput().GetMinBlobBytes(),
			ExcludedSmallOps: pb.GetThroughput().GetExcludedSmallOps(),
		},
	}
}

func histogramFromProto(pb *session.Histogram) HistogramSnapshot {
	return HistogramSnapshot{
		Boundaries: pb.GetBoundaries(),
		Counts:     pb.GetCounts(),
		Count:      pb.GetCount(),
		Sum:        pb.GetSum(),
		Min:        pb.GetMin(),
		Max:        pb.GetMax(),
	}
}
