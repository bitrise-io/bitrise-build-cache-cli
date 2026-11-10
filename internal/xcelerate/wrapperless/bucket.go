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
}
