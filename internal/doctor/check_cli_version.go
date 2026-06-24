package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

func (d *Doctor) cliVersionCheck() Check {
	return Check{
		Name: "cli-version",
		Diagnose: func(ctx context.Context) Result {
			current := d.CLIVersion
			if current == "" {
				current = "devel"
			}

			if isLocalBuild(current) {
				return Result{
					State:  StateWarn,
					Detail: "current=" + current + " — local build, not a tagged release. Install a release via the installer script or `brew install bitrise-io/bitrise-build-cache/bitrise-build-cache` unless you're hotfixing.",
				}
			}

			latest, err := d.LatestReleaseTag(ctx, d.HTTPClient)
			if err != nil {
				return Result{State: StateWarn, Detail: fmt.Sprintf("current=%s; could not check latest (%v)", current, err)}
			}
			if latest == "" {
				return Result{State: StateOK, Detail: "current=" + current}
			}

			if current == latest || strings.TrimPrefix(current, "v") == strings.TrimPrefix(latest, "v") {
				return Result{State: StateOK, Detail: "current=" + current + " (up to date)"}
			}

			return Result{
				State:  StateWarn,
				Detail: fmt.Sprintf("current=%s, latest=%s — run `bitrise-build-cache update` (detects brew vs installer.sh and runs the right flow)", current, latest),
			}
		},
	}
}

func isLocalBuild(version string) bool {
	return version == "devel" ||
		strings.Contains(version, "+") ||
		strings.Contains(version, "dirty")
}

func fetchLatestGitHubRelease(ctx context.Context, client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/bitrise-io/bitrise-build-cache-cli/releases/latest", nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return "", fmt.Errorf("timeout: %w", err)
		}

		return "", fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases API returned %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode release payload: %w", err)
	}

	return payload.TagName, nil
}
