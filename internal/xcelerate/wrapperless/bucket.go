package wrapperless

import (
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

type BucketReason int

const (
	BucketReasonUnknown BucketReason = iota
	BucketReasonEndMatched
	BucketReasonEndOfPrior
	BucketReasonIDEBackground
	BucketReasonCLIImplicit
	// BucketReasonSpeculative marks a provisional bucket opened to keep
	// Proxy.CurrentBucketID live between builds. Promoted / classified at the
	// next Start / End / Quit and never emitted as-is.
	BucketReasonSpeculative
)

type Bucket struct {
	InvocationID    string              `json:"invocationId"`
	StartAt         time.Time           `json:"startAt"`
	EndAt           time.Time           `json:"endAt"`
	Scheme          string              `json:"scheme,omitempty"`
	Project         string              `json:"project,omitempty"`
	Workspace       string              `json:"workspace,omitempty"`
	Configuration   string              `json:"configuration,omitempty"`
	Action          PrimaryAction       `json:"action,omitempty"`
	Success         bool                `json:"success"`
	SuccessKnown    bool                `json:"successKnown"`
	Duration        time.Duration       `json:"durationNs,omitempty"`
	Reason          BucketReason        `json:"reason"`
	BuildRequestHex string              `json:"buildRequestHex,omitempty"`
	PeerPID         int                 `json:"peerPid,omitempty"`
	PeerStartMS     int64               `json:"peerStartMs,omitempty"`
	BlobStats       *blobstats.Snapshot `json:"blobStats,omitempty"`
	// XCActivityLogPath copied from the matching End; empty for Start-only
	// buckets (CLI-implicit / IDE-background) because those have no manifest.
	XCActivityLogPath string `json:"xcActivityLogPath,omitempty"`
}
