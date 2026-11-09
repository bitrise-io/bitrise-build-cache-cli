package wrapperless

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	}
}

func (e *Emitter) EmitEnriched(bucket Bucket, hitRate float32) {
	if e == nil || e.Client == nil {
		return
	}

	inv := e.buildInvocation(bucket, PhaseEnriched, hitRate)
	if err := e.Client.PutInvocation(inv); err != nil {
		e.logWarn("wrapperless enriched PUT %s failed: %s", inv.InvocationID, err)
	}
}

func (e *Emitter) buildInvocation(bucket Bucket, phase Phase, hitRate float32) analytics.Invocation {
	now := e.now()

	start := bucket.StartAt
	if start.IsZero() {
		start = now
	}

	cmd := shortCommand(bucket)
	run := analytics.InvocationRunStats{
		InvocationDate:   start,
		InvocationID:     bucket.InvocationID,
		Duration:         bucket.Duration.Milliseconds(),
		Command:          cmd,
		FullCommand:      cmd,
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

// shortCommand renders "<action> [<scheme>]" when a scheme is known,
// otherwise just the action verb.
func shortCommand(bucket Bucket) string {
	action := string(bucket.Action)
	if action == "" {
		action = "build"
	}
	if bucket.Scheme == "" {
		return action
	}

	return action + " [" + bucket.Scheme + "]"
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
