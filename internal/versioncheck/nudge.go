package versioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GitHubReleasesURL is the public GitHub API endpoint for the latest release
// of the CLI repo. Used by FetchLatestVersion. Constructed as a constant so
// tests can replace it via the URL parameter on FetchLatestVersion.
const GitHubReleasesURL = "https://api.github.com/repos/bitrise-io/bitrise-build-cache-cli/releases/latest"

// NudgeCooldown is the minimum time between two release-lookup network calls
// for the same user. Keeps the GitHub API call cheap on the hot path while
// still surfacing new releases within a day.
const NudgeCooldown = 24 * time.Hour

// FetchTimeout caps the HTTP call so a flaky network doesn't slow every CLI
// run. The version check is best-effort by design.
//
// Aligned with the context timeout the root cobra hook attaches in
// cmd/common/root.go (see RunVersionCheck). Keep them in sync — diverging
// values create a confusing window where http.Client times out one way
// and ctx the other.
const FetchTimeout = 3 * time.Second

// ErrThrottled is returned by FetchLatestVersion when GitHub responds with
// 403 / 429 (rate-limit, common on corporate-NAT shared IPs). Callers MUST
// advance LastNudgeAt anyway to avoid hammering GitHub every invocation —
// the throttle response is a signal that the cooldown should kick in, not
// a "retry me" error.
var ErrThrottled = errors.New("github rate-limited the release lookup (403/429)")

// ErrNudgeSuppressed is returned by ShouldNudge when the user has opted out
// (--no-update-check, CI=true) or when we've already nudged inside the
// cooldown window. The CLI surface ignores this error — it's a signal, not a
// failure.
var ErrNudgeSuppressed = errors.New("version-check nudge suppressed")

// NudgeDecision captures the inputs that gate whether we hit the network on
// a given run. Pure data so tests can construct decisions without environment
// poisoning.
type NudgeDecision struct {
	NoUpdateCheckFlag bool
	IsCI              bool
	Now               time.Time
	LastNudgeAt       time.Time
}

// ShouldNudge returns nil if a network lookup is appropriate this run, or
// ErrNudgeSuppressed otherwise. Caller treats ErrNudgeSuppressed as "skip"
// without propagating it as a CLI error.
func ShouldNudge(d NudgeDecision) error {
	if d.NoUpdateCheckFlag {
		return ErrNudgeSuppressed
	}

	if d.IsCI {
		return ErrNudgeSuppressed
	}

	if !d.LastNudgeAt.IsZero() && d.Now.Sub(d.LastNudgeAt) < NudgeCooldown {
		return ErrNudgeSuppressed
	}

	return nil
}

// releaseResponse is the subset of GitHub's release JSON we read. Everything
// else is ignored — additive fields stay safe.
type releaseResponse struct {
	TagName string `json:"tag_name"`
}

// FetchLatestVersion calls the GitHub releases API and returns the bare
// version string (with any leading `v` stripped — Goreleaser tags as `vX.Y.Z`
// but our internal version is `X.Y.Z`). Errors are returned to the caller,
// which treats them as "we'll try again next run" — the version check must
// never block the user.
//
// url is parameterised so tests can swap in a test server URL. Pass
// GitHubReleasesURL in production.
func FetchLatestVersion(ctx context.Context, client *http.Client, url string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: FetchTimeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build release request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "bitrise-build-cache-cli/version-check")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		// 403 + 429 are GitHub's rate-limit signals (unauth API allows ~60
		// req/h per IP — corporate NAT shares the budget across the office,
		// so we see this in the wild). Return ErrThrottled so Run advances
		// LastNudgeAt; without that, every CLI invocation would hammer
		// GitHub forever once the budget was blown.
		return "", ErrThrottled
	}

	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("github responded %d for latest release", resp.StatusCode)
	}

	var body releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode release response: %w", err)
	}

	tag := strings.TrimSpace(body.TagName)
	if tag == "" {
		return "", errors.New("github release response has empty tag_name")
	}

	return strings.TrimPrefix(tag, "v"), nil
}

// IsBehind compares the running and latest versions textually. We don't run
// real semver compare here because the CLI's `devel` and snapshot versions
// don't follow strict semver; instead we treat "non-empty and different from
// latest" as behind. The nudge UX is informational, so a false-positive on a
// pre-release build is benign.
//
// Returns false when current == latest or when either side is the "devel"
// sentinel (local builds shouldn't nag).
func IsBehind(current, latest string) bool {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)

	if current == "" || latest == "" {
		return false
	}

	if current == "devel" {
		return false
	}

	if strings.TrimPrefix(current, "v") == strings.TrimPrefix(latest, "v") {
		return false
	}

	return true
}
