package analytics

import (
	"encoding/json"
	"fmt"
	"time"
)

// ParseCcacheStats parses the JSON output of `ccache --print-stats --format=json`.
// CacheHitRate is computed from direct and preprocessed hits over total attempts.
func ParseCcacheStats(data []byte) (CcacheStats, error) {
	var stats CcacheStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return CcacheStats{}, fmt.Errorf("parse ccache stats JSON: %w", err)
	}

	total := stats.DirectCacheHit + stats.PreprocessedCacheHit + stats.CacheMiss
	if total > 0 {
		stats.CacheHitRate = float64(stats.DirectCacheHit+stats.PreprocessedCacheHit) / float64(total)
	}

	return stats, nil
}

// NewCcacheInvocation assembles a CcacheInvocation from a ccache stats snapshot and transfer byte counts.
// It references the parent Invocation via parentInvocationID and contains only ccache-specific data.
func NewCcacheInvocation(invocationID, parentInvocationID string, invocationDate time.Time, stats CcacheStats, downloadedBytes, uploadedBytes int64) *CcacheInvocation {
	return &CcacheInvocation{
		InvocationID:       invocationID,
		ParentInvocationID: parentInvocationID,
		InvocationDate:     invocationDate,
		CcacheStats:        stats,
		DownloadedBytes:    downloadedBytes,
		UploadedBytes:      uploadedBytes,
	}
}

// PutCcacheInvocation sends a CcacheInvocation to the analytics backend via HTTP PUT.
func (c *Client) PutCcacheInvocation(inv CcacheInvocation) error {
	if err := c.Put(fmt.Sprintf("/ccache-invocations/%s", inv.InvocationID), inv); err != nil {
		return fmt.Errorf("put ccache invocation: %w", err)
	}

	return nil
}
