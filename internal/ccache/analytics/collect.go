package analytics

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
)

// CollectAndZero runs ccache --print-stats, sends a CcacheInvocation to the analytics
// backend, then zeros ccache's counters. Zeroing only happens after a successful send
// so stats are never silently discarded. Errors are logged but do not fail the caller.
func CollectAndZero(ctx context.Context, client *Client, invocationID, parentID string, downloadedBytes, uploadedBytes int64, logger log.Logger) {
	if err := collectAndSend(ctx, client, invocationID, parentID, downloadedBytes, uploadedBytes); err != nil {
		logger.TErrorf("Skipping ccache stats reset because collection/send failed: %v", err)

		return
	}

	zeroCcacheStats(ctx, logger)
}

func collectAndSend(ctx context.Context, client *Client, invocationID, parentID string, dl, ul int64) error {
	if parentID != "" {
		rel := multiplatform.InvocationRelation{
			ParentInvocationID: parentID,
			ChildInvocationID:  invocationID,
			InvocationDate:     time.Now(),
			BuildTool:          "ccache",
		}
		if err := client.PutInvocationRelation(rel); err != nil {
			return fmt.Errorf("register invocation relation: %w", err)
		}
	}

	ccachePath, lookErr := exec.LookPath("ccache")
	if lookErr != nil {
		// ccache binary not available — still report transfer bytes if we have them
		if dl == 0 && ul == 0 {
			return nil
		}

		inv := NewCcacheInvocation(invocationID, parentID, time.Now(), CcacheStats{}, dl, ul)

		return client.PutCcacheInvocation(*inv)
	}

	statsData, err := exec.CommandContext(ctx, ccachePath, "--print-stats", "--format=json").Output() //nolint:gosec
	if err != nil {
		return fmt.Errorf("collect ccache stats: %w", err)
	}

	stats, err := ParseCcacheStats(statsData)
	if err != nil {
		return fmt.Errorf("parse ccache stats: %w", err)
	}

	inv := NewCcacheInvocation(invocationID, parentID, time.Now(), stats, dl, ul)

	return client.PutCcacheInvocation(*inv)
}

func zeroCcacheStats(ctx context.Context, logger log.Logger) {
	ccachePath, err := exec.LookPath("ccache")
	if err != nil {
		return
	}

	if err := exec.CommandContext(ctx, ccachePath, "-z").Run(); err != nil { //nolint:gosec
		logger.TErrorf("Failed to reset ccache stats: %v", err)
	}
}
