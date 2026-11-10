package wrapperless

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

//go:generate moq -stub -out invocation_putter_mock_test.go -pkg wrapperless_test . InvocationPutter

type InvocationPutter interface {
	PutInvocation(inv analytics.Invocation) error
}

type Phase string

const (
	PhaseSlim     Phase = "slim"
	PhaseEnriched Phase = "enriched"
)

const (
	defaultSlimRetryBudget = 3
	defaultSlimBackoff     = 200 * time.Millisecond
)

// Payload is the wrapperless emitter's single row representation; Phase
// distinguishes the two PUT points for the same InvocationID.
type Payload struct {
	Phase   Phase   `json:"phase"`
	Bucket  Bucket  `json:"bucket"`
	HitRate float32 `json:"hitRate"`
}

// Emitter owns the two-phase PUT path. Slim arms the row the moment a Start
// signal is observed; Enriched updates it with the matched End + CAS stats.
type Emitter struct {
	Client           InvocationPutter
	Auth             auth.Credential
	Metadata         configcommon.CacheConfigMetadata
	XcodeVersion     string
	XcodeBuildNumber string
	Logger           log.Logger
	PendingSlimPath  string
	Now              func() time.Time
	SlimRetryBudget  int
	SlimBackoff      time.Duration

	mu sync.Mutex
}

func (e *Emitter) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now()
}

func (e *Emitter) EmitSlim(bucket Bucket) {
	if e == nil || e.Client == nil {
		return
	}

	inv := e.buildInvocation(bucket, PhaseSlim, 0)

	if err := e.putWithRetry(inv); err != nil {
		e.logWarn("wrapperless slim PUT %s failed after retries: %s", inv.InvocationID, err)
		if persisted := e.persistSlim(Payload{Phase: PhaseSlim, Bucket: bucket}); !persisted {
			e.logWarn("wrapperless slim pending persist %s dropped", inv.InvocationID)
		}

		return
	}

	e.logDebug("Emitter: slim PUT id=%s scheme=%s", inv.InvocationID, bucket.Scheme)
}

func (e *Emitter) EmitEnriched(bucket Bucket, hitRate float32) {
	if e == nil || e.Client == nil {
		return
	}

	inv := e.buildInvocation(bucket, PhaseEnriched, hitRate)
	if err := e.Client.PutInvocation(inv); err != nil {
		e.logWarn("wrapperless enriched PUT %s failed: %s", inv.InvocationID, err)

		return
	}

	e.logDebug("Emitter: enriched PUT id=%s scheme=%s bytes=%d hitRate=%.2f",
		inv.InvocationID, bucket.Scheme, snapshotBytes(bucket.BlobStats), hitRate)
}

func (e *Emitter) buildInvocation(bucket Bucket, phase Phase, hitRate float32) analytics.Invocation {
	now := e.now()

	start := bucket.StartAt
	if start.IsZero() {
		start = now
	}

	run := analytics.InvocationRunStats{
		InvocationDate:   start,
		InvocationID:     bucket.InvocationID,
		Duration:         bucket.Duration.Milliseconds(),
		Command:          shortCommand(bucket),
		FullCommand:      fullCommand(bucket),
		XcodeVersion:     e.XcodeVersion,
		XcodeBuildNumber: e.XcodeBuildNumber,
	}

	if phase == PhaseEnriched {
		run.HitRate = hitRate
		run.Success = bucket.Success
		run.CacheBlobStats = bucket.BlobStats
		if !bucket.Success && bucket.SuccessKnown {
			run.Error = errors.New("xcode build failed")
		}
	}

	inv := analytics.NewInvocation(run, e.Auth, e.Metadata)

	return *inv
}

// shortCommand renders the BE row's command string. Dedicated reasons
// (IDE-background, CLI-implicit) carry a fixed label so the BE row never
// masquerades as a user build; otherwise it's "<action> [<scheme> / <config>]"
// matching the wrapper flow's convention. Configuration is appended only when
// present; scheme-only renders as "<action> [<scheme>]".
func shortCommand(bucket Bucket) string {
	switch bucket.Reason {
	case BucketReasonIDEBackground:
		return "ide background"
	case BucketReasonCLIImplicit:
		return "cli run"
	case BucketReasonUnknown, BucketReasonEndMatched, BucketReasonEndOfPrior:
	}

	action := actionVerb(bucket)
	if bucket.Scheme == "" {
		return action
	}

	target := bucket.Scheme
	if bucket.Configuration != "" {
		target += " / " + bucket.Configuration
	}

	return action + " [" + target + "]"
}

// fullCommand renders an argv-shaped xcodebuild invocation when enough
// metadata was captured from the LogStoreManifest signature; falls back to
// shortCommand when it would be a bare verb.
func fullCommand(bucket Bucket) string {
	switch bucket.Reason {
	case BucketReasonIDEBackground, BucketReasonCLIImplicit:
		return shortCommand(bucket)
	case BucketReasonUnknown, BucketReasonEndMatched, BucketReasonEndOfPrior:
	}

	if bucket.Scheme == "" && bucket.Workspace == "" && bucket.Project == "" {
		return shortCommand(bucket)
	}

	parts := []string{"xcodebuild", actionVerb(bucket)}

	switch {
	case bucket.Workspace != "":
		parts = append(parts, "-workspace", bucket.Workspace)
	case bucket.Project != "":
		parts = append(parts, "-project", bucket.Project)
	}

	if bucket.Scheme != "" {
		parts = append(parts, "-scheme", bucket.Scheme)
	}

	if bucket.Configuration != "" {
		parts = append(parts, "-configuration", bucket.Configuration)
	}

	return strings.Join(parts, " ")
}

func actionVerb(bucket Bucket) string {
	if a := string(bucket.Action); a != "" {
		return a
	}

	return "build"
}

func (e *Emitter) putWithRetry(inv analytics.Invocation) error {
	budget := e.SlimRetryBudget
	if budget <= 0 {
		budget = defaultSlimRetryBudget
	}
	backoff := e.SlimBackoff
	if backoff <= 0 {
		backoff = defaultSlimBackoff
	}

	var lastErr error
	for attempt := range budget {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}

		if err := e.Client.PutInvocation(inv); err != nil {
			lastErr = err

			continue
		}

		return nil
	}

	if lastErr == nil {
		lastErr = errors.New("slim PUT retry budget exhausted")
	}

	return lastErr
}

func (e *Emitter) persistSlim(p Payload) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	path := e.PendingSlimPath
	if path == "" {
		return false
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.logWarn("wrapperless slim persist mkdir %s: %s", filepath.Dir(path), err)

		return false
	}

	body, err := json.Marshal(p)
	if err != nil {
		e.logWarn("wrapperless slim persist marshal: %s", err)

		return false
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // app-owned state file
	if err != nil {
		e.logWarn("wrapperless slim persist open %s: %s", path, err)

		return false
	}
	defer f.Close() //nolint:errcheck

	if _, err := fmt.Fprintln(f, string(body)); err != nil {
		e.logWarn("wrapperless slim persist write %s: %s", path, err)

		return false
	}

	return true
}

func (e *Emitter) logWarn(format string, args ...any) {
	if e.Logger == nil {
		return
	}

	e.Logger.Warnf(format, args...)
}

func (e *Emitter) logDebug(format string, args ...any) {
	if e.Logger == nil {
		return
	}

	e.Logger.Debugf(format, args...)
}
