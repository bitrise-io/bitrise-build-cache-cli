package versioncheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
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
	// Out is where the nudge message is written when a behind-latest release
	// is detected. Typically os.Stderr (so JSON-parsing stdout consumers like
	// react-native CLI wrappers aren't disturbed).
	Out io.Writer
	// Now is the wall-clock used for cooldown comparisons; tests inject a
	// fixed time. Defaults to time.Now() when zero.
	Now time.Time
	// HTTPClient is the network client used for the GitHub release lookup.
	// Tests inject a client pointed at a test server; production uses a
	// 2-second-timeout client.
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
//     writes a one-line nudge to Out when behind.
func Run(ctx context.Context, opts Options) (Result, error) {
	var result Result

	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	if opts.FetchURL == "" {
		opts.FetchURL = GitHubReleasesURL
	}

	if opts.Out == nil {
		opts.Out = os.Stderr
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
		// MUST see nil here so the CLI doesn't surface it as an error. Save
		// state with the old LastNudgeAt and return cleanly.
		_ = SaveState(opts.Home, newState)

		return result, nil //nolint:nilerr // ErrNudgeSuppressed is intentionally swallowed
	}

	latest, err := FetchLatestVersion(ctx, opts.HTTPClient, opts.FetchURL)
	result.NetworkCalled = true

	if err != nil {
		// Best-effort: save state without bumping LastNudgeAt so we'll retry
		// on the next run (network blips don't permanently stop us).
		_ = SaveState(opts.Home, newState)

		return result, err
	}

	result.LatestVersion = latest
	result.Behind = IsBehind(opts.CurrentVersion, latest)

	if result.Behind {
		writeNudge(opts.Out, opts.CurrentVersion, latest)
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

// writeNudge emits the user-facing message. One line, written to Out
// (stderr in production) so JSON-parsing stdout consumers aren't disturbed.
func writeNudge(w io.Writer, current, latest string) {
	_, _ = fmt.Fprintf(w,
		"Bitrise Build Cache CLI %s is available (you're running %s). Run `bitrise-build-cache update` or `brew upgrade bitrise-build-cache-cli` to upgrade. Pass --no-update-check to silence.\n",
		latest, current,
	)
}
