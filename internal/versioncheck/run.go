package versioncheck

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

// Options bundles the inputs the high-level Run helper needs. Kept as a
// struct so callers (cmd/common/root.go) can populate it from flags + env
// without a sprawling positional argument list.
type Options struct {
	// CurrentVersion is the running binary's resolved CLI version.
	CurrentVersion string
	// Home is the user's home directory. Use os.UserHomeDir() at the call
	// site, or t.TempDir() in tests.
	Home string
	// NoUpdateCheck is true when the user passed --no-update-check.
	NoUpdateCheck bool
	// Logger receives the user-facing nudge when a behind-latest release is
	// detected. Production callers pass a logger writing to stderr (so
	// JSON-parsing stdout consumers like react-native CLI wrappers aren't
	// disturbed); tests pass log.NewLogger(log.WithOutput(&buf)). MUST be
	// non-nil.
	Logger log.Logger
	// Now is the wall-clock used for cooldown comparisons; tests inject a
	// fixed time. Defaults to time.Now() when zero.
	Now time.Time
	// HTTPClient is the network client used for the GitHub release lookup.
	// Tests inject a client pointed at a test server; production uses a
	// 3-second-timeout client.
	HTTPClient *http.Client
	// FetchURL is the GitHub release endpoint. Tests override to point at a
	// test server. Defaults to GitHubReleasesURL when empty.
	FetchURL string
	// IsCI is true when the CLI is running under CI. Suppresses the nudge.
	// Caller passes the result of common-environment detection so we don't
	// duplicate the heuristic here.
	IsCI bool
}

// runOnce guards Run so the version check fires at most once per process,
// even if multiple PersistentPreRun hooks happen to call it (defensive — the
// hook should be the only entry).
//
//nolint:gochecknoglobals
var runOnce sync.Once

// Result describes what Run did. Returned for callers / tests that want to
// branch on the outcome; in production main.go path the result is ignored.
type Result struct {
	Drift         DriftResult
	NetworkCalled bool
	Behind        bool
	LatestVersion string
}

// Run is the high-level entry point hooked into the root PersistentPreRun.
// Returns a Result describing what happened. Errors are returned but the
// caller MUST treat them as advisory — the version check is best-effort and
// must never block a CLI invocation.
//
// Behaviour:
//   - Loads persisted state (silently treats missing state as first run).
//   - Detects drift; persists the running version so the next run sees it.
//   - When network nudging is allowed (not --no-update-check, not CI,
//     cooldown elapsed), fetches the latest release tag from GitHub and
//     logs a one-line nudge when behind.
func Run(ctx context.Context, opts Options) (Result, error) {
	var result Result

	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	if opts.FetchURL == "" {
		opts.FetchURL = GitHubReleasesURL
	}

	state, _ := LoadState(opts.Home) // first-run absent file is fine
	result.Drift = Detect(state, opts.CurrentVersion)

	// Always persist the running version + bump the last-seen timestamp.
	// LastNudgeAt only advances when we actually nudge below.
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
		// ErrNudgeSuppressed is a "skip" signal, not a failure; the caller
		// MUST see nil here so the CLI doesn't surface it as an error.
		//
		// Hot-path optimisation: when the drift is NoChange (running version
		// matches persisted) and LastNudgeAt isn't moving, the on-disk file
		// is already what we'd write. Skip the mkdir + temp-file +
		// JSON-marshal + atomic-rename that SaveState does on every call.
		if result.Drift.Kind != NoChange {
			_ = SaveState(opts.Home, newState)
		}

		return result, nil //nolint:nilerr // ErrNudgeSuppressed is intentionally swallowed
	}

	latest, err := FetchLatestVersion(ctx, opts.HTTPClient, opts.FetchURL)
	result.NetworkCalled = true

	if err != nil {
		// Throttle response (GitHub 403/429): treat as "we already nudged"
		// so the next NudgeCooldown window passes before we retry. Without
		// this, a corporate NAT that's blown the unauthenticated API
		// budget would hit GitHub on every single CLI invocation forever.
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

// RunOnce wraps Run in a sync.Once so the check fires at most once per
// process even if multiple cobra subcommands re-enter the hook.
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

// writeNudge emits the user-facing "newer release available" message. Uses
// Warnf because the nudge is informational-but-actionable: not an error
// (the CLI still runs), but the user should consider upgrading.
func writeNudge(logger log.Logger, current, latest string) {
	if logger == nil {
		return
	}

	logger.Warnf(
		"Bitrise Build Cache CLI %s is available (you're running %s). Run `bitrise-build-cache update` or `brew upgrade bitrise-io/bitrise-build-cache/bitrise-build-cache` to upgrade. Pass --no-update-check to silence.",
		latest, current,
	)
}
