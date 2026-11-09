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
)

type Bucket struct {
	InvocationID    string
	StartAt         time.Time
	EndAt           time.Time
	Scheme          string
	Action          PrimaryAction
	Success         bool
	SuccessKnown    bool
	Duration        time.Duration
	Reason          BucketReason
	BuildRequestHex string
	PeerPID         int
	PeerStartMS     int64
	BlobStats       *blobstats.Snapshot
}
