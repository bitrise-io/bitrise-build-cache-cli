package versioncheck

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

type Options struct {
	CurrentVersion string
	Home           string
	NoUpdateCheck  bool
	// Logger must be non-nil. Writes to stderr in production (so JSON-stdout consumers aren't disturbed).
	Logger     log.Logger
	Now        time.Time
	HTTPClient *http.Client
	FetchURL   string
	IsCI       bool
}

//nolint:gochecknoglobals
var runOnce sync.Once

type Result struct {
	Drift         DriftResult
	NetworkCalled bool
	Behind        bool
	LatestVersion string
}

// Run is best-effort — caller must treat any error as advisory.
func Run(ctx context.Context, opts Options) (Result, error) {
	var result Result

	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	if opts.FetchURL == "" {
		opts.FetchURL = GitHubReleasesURL
	}

	state, _ := LoadState(opts.Home)
	result.Drift = Detect(state, opts.CurrentVersion)

	newState := State{
		LastVersion: opts.CurrentVersion,
		LastSeenAt:  opts.Now,
		LastNudgeAt: state.LastNudgeAt,
	}

	if err := ShouldNudge(NudgeDecision{
		NoUpdateCheckFlag: opts.NoUpdateCheck,
		IsCI:              opts.IsCI,
		Now:               opts.Now,
		LastNudgeAt:       state.LastNudgeAt,
	}); err != nil {
		// Hot path: skip SaveState when no version change AND no nudge — file already matches.
		if result.Drift.Kind != NoChange {
			_ = SaveState(opts.Home, newState)
		}

		return result, nil //nolint:nilerr // ErrNudgeSuppressed is intentionally swallowed
	}

	latest, err := FetchLatestVersion(ctx, opts.HTTPClient, opts.FetchURL)
	result.NetworkCalled = true

	if err != nil {
		// On 403/429 advance LastNudgeAt so a NAT that blew the unauthenticated API budget doesn't hit GitHub every invocation.
		if errors.Is(err, ErrThrottled) {
			newState.LastNudgeAt = opts.Now
		}

		_ = SaveState(opts.Home, newState)

		return result, err
	}

	result.LatestVersion = latest
	result.Behind = IsBehind(opts.CurrentVersion, latest)

	if result.Behind {
		writeNudge(opts.Logger, opts.CurrentVersion, latest)
		newState.LastNudgeAt = opts.Now
	}

	if err := SaveState(opts.Home, newState); err != nil {
		return result, err
	}

	return result, nil
}

func RunOnce(ctx context.Context, opts Options) (Result, error) {
	var (
		res Result
		err error
	)

	runOnce.Do(func() {
		res, err = Run(ctx, opts)
	})

	return res, err
}

func writeNudge(logger log.Logger, current, latest string) {
	if logger == nil {
		return
	}

	logger.Warnf(
		"Bitrise Build Cache CLI %s is available (you're running %s). Run `bitrise-build-cache update` or `brew upgrade bitrise-io/bitrise-build-cache/bitrise-build-cache` to upgrade. Pass --no-update-check to silence.",
		latest, current,
	)
}
