package versioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const GitHubReleasesURL = "https://api.github.com/repos/bitrise-io/bitrise-build-cache-cli/releases/latest"

const NudgeCooldown = 4 * time.Hour

// FetchTimeout must stay in sync with the ctx timeout in cmd/common/root.go to avoid mismatched timeout windows.
const FetchTimeout = 3 * time.Second

// Callers MUST advance LastNudgeAt on ErrThrottled so a NAT that blew the unauth API budget doesn't hit GitHub on every run.
var ErrThrottled = errors.New("github rate-limited the release lookup (403/429)")

var ErrNudgeSuppressed = errors.New("version-check nudge suppressed")

type NudgeDecision struct {
	NoUpdateCheckFlag bool
	IsCI              bool
	Now               time.Time
	LastNudgeAt       time.Time
}

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

type releaseResponse struct {
	TagName string `json:"tag_name"`
}

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
		return "", ErrThrottled
	}

	if resp.StatusCode != http.StatusOK {
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

// IsBehind suppresses the nudge for local builds and invalid-semver inputs.
func IsBehind(current, latest string) bool {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)

	if current == "" || latest == "" {
		return false
	}

	if isLocalBuild(current) {
		return false
	}

	curV := ensureSemverPrefix(current)
	latestV := ensureSemverPrefix(latest)
	if !semver.IsValid(curV) || !semver.IsValid(latestV) {
		return false
	}

	return semver.Compare(curV, latestV) < 0
}

func isLocalBuild(v string) bool {
	return v == "devel" || strings.Contains(v, "+") || strings.Contains(v, "dirty")
}

func ensureSemverPrefix(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}

	return "v" + v
}
