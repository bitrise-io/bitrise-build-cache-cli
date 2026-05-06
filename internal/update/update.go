// Package update checks for newer CLI releases on GitHub and applies them
// by downloading the matching tarball and atomically replacing the running
// binary.
package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	githubLatestAPI = "https://api.github.com/repos/bitrise-io/bitrise-build-cache-cli/releases/latest"
	binaryName      = "bitrise-build-cache"
)

// Release is the subset of the GitHub releases API response we care about.
type Release struct {
	TagName string `json:"tag_name"` // e.g. "v0.19.0"
	HTMLURL string `json:"html_url"`
}

// FetchLatestRelease calls the GitHub releases API and returns the latest
// non-prerelease release. The returned TagName includes the leading "v".
func FetchLatestRelease(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestAPI, nil)
	if err != nil {
		return Release{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "bitrise-build-cache-cli")

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("github API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github API returned HTTP %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("decode response: %w", err)
	}

	return release, nil
}

// IsNewer reports whether latestTag (e.g. "v0.19.0") is strictly newer than
// currentVersion (e.g. "0.18.0" — without leading "v").
// Returns false for dev/unknown versions so we don't spuriously prompt.
func IsNewer(latestTag, currentVersion string) bool {
	latest := strings.TrimPrefix(latestTag, "v")

	if currentVersion == "" || currentVersion == "(devel)" {
		return false
	}

	lp := parseSemver(latest)
	cp := parseSemver(currentVersion)

	for i := range lp {
		if lp[i] > cp[i] {
			return true
		}

		if lp[i] < cp[i] {
			return false
		}
	}

	return false
}

// Apply downloads the release tarball for version (without leading "v") and
// atomically replaces the running binary. If version is empty it fetches the
// latest from GitHub first.
func Apply(ctx context.Context, version string) error {
	if version == "" {
		release, err := FetchLatestRelease(ctx)
		if err != nil {
			return fmt.Errorf("fetch latest release: %w", err)
		}

		version = strings.TrimPrefix(release.TagName, "v")
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	url := assetURL(version, runtime.GOOS, runtime.GOARCH)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	client := &http.Client{Timeout: 120 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d for %s", resp.StatusCode, url)
	}

	dir := filepath.Dir(exePath)

	tmp, err := os.CreateTemp(dir, binaryName+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if err := extractBinary(resp.Body, binaryName, tmp); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("extract binary: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("replace binary: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

func assetURL(version, goos, goarch string) string {
	return fmt.Sprintf(
		"https://github.com/bitrise-io/bitrise-build-cache-cli/releases/download/v%s/%s_%s_%s_%s.tar.gz",
		version, binaryName, version, goos, goarch,
	)
}

func extractBinary(r io.Reader, name string, dst io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s not found in archive", name)
		}

		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		if filepath.Base(hdr.Name) == name && hdr.Typeflag == tar.TypeReg {
			if _, err := io.Copy(dst, io.LimitReader(tr, hdr.Size)); err != nil {
				return fmt.Errorf("write binary: %w", err)
			}

			return nil
		}
	}
}

func parseSemver(v string) [3]int {
	var result [3]int
	parts := strings.SplitN(v, ".", 3)

	for i, p := range parts {
		if i >= 3 {
			break
		}

		n, _ := strconv.Atoi(p)
		result[i] = n
	}

	return result
}
