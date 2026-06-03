// Package status implements the "is it working right now?" check that backs
// the `bitrise-build-cache status` subcommand.
//
// Each check is a small function returning a Check value. The runner gathers
// all checks into a Status struct that can be rendered human-readably or as
// JSON. Individual check failures don't abort the run — partial results are
// the point.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// State is the severity of a single Check.
type State string

const (
	StateOK    State = "ok"
	StateWarn  State = "warn"
	StateError State = "error"
)

// Check is a single status result.
type Check struct {
	Name   string `json:"name"`
	State  State  `json:"state"`
	Detail string `json:"detail"`
}

// Status is the aggregated result returned by Run.
type Status struct {
	Checks  []Check `json:"checks"`
	Version string  `json:"cli_version"`
}

// Overall returns the worst state across all checks.
func (s Status) Overall() State {
	worst := StateOK
	for _, c := range s.Checks {
		switch c.State {
		case StateError:
			return StateError
		case StateWarn:
			worst = StateWarn
		case StateOK:
		}
	}

	return worst
}

// authLoader is the slice of *keychain.Keychain we depend on (DI for tests).
type authLoader interface {
	Load() (keychain.Credentials, error)
}

// Runner aggregates the individual checks. Each field can be overridden for
// tests; nil means "use the default implementation".
type Runner struct {
	OsProxy    utils.OsProxy
	AuthLoader authLoader
	Envs       map[string]string
	CLIVersion string
	HTTPClient *http.Client
	// LatestReleaseTag returns the latest released CLI tag (e.g. "v2.8.4"),
	// or "" + nil error when the lookup is intentionally skipped. Errors bubble
	// up to the check as a warn.
	LatestReleaseTag func(ctx context.Context, c *http.Client) (string, error)
	// XcelerateProxyDir returns the directory the xcelerate proxy writes its
	// pid file into. Default uses xcelerate.DirPath.
	XcelerateProxyDir func() string
}

// NewRunner returns a Runner with production defaults.
func NewRunner() *Runner {
	osProxy := utils.DefaultOsProxy{}

	return &Runner{
		OsProxy:           osProxy,
		AuthLoader:        keychain.New(),
		Envs:              utils.AllEnvs(),
		CLIVersion:        common.GetCLIVersion(nil),
		HTTPClient:        &http.Client{Timeout: 3 * time.Second},
		LatestReleaseTag:  fetchLatestGitHubRelease,
		XcelerateProxyDir: func() string { return xcelerate.DirPath(osProxy) },
	}
}

// Run executes every check in order and returns the aggregated Status.
func (r *Runner) Run(ctx context.Context) Status {
	checks := []Check{
		r.checkXcelerateProxy(),
		r.checkCcacheHelper(ctx),
		r.checkAuth(),
		r.checkCLIVersion(ctx),
	}

	return Status{
		Checks:  checks,
		Version: r.CLIVersion,
	}
}

func (r *Runner) checkCcacheHelper(ctx context.Context) Check {
	const name = "ccache-helper"

	socketPath := r.Envs["BITRISE_CCACHE_IPC_SOCKET_PATH"]
	if socketPath == "" {
		socketPath = os.TempDir() + "/ccache-ipc.sock"
	}

	if _, err := os.Stat(socketPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Check{
				Name:   name,
				State:  StateWarn,
				Detail: "not running (no socket file). Run `bitrise-build-cache ccache start-storage-helper` if you're using ccache.",
			}
		}

		return Check{Name: name, State: StateError, Detail: "stat ccache socket: " + err.Error()}
	}

	// Probe the socket — proves a process is actually listening, not just a
	// stale inode left after a crash.
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	conn, err := dialer.DialContext(probeCtx, "unix", socketPath)
	if err != nil {
		return Check{
			Name:   name,
			State:  StateError,
			Detail: fmt.Sprintf("socket %s present but not accepting connections (%v). Remove the socket or restart the helper.", socketPath, err),
		}
	}
	_ = conn.Close()

	return Check{Name: name, State: StateOK, Detail: "running (" + socketPath + ")"}
}

func (r *Runner) checkXcelerateProxy() Check {
	const name = "xcelerate-proxy"

	pidFile := r.XcelerateProxyDir() + "/proxy.pid"

	content, err := os.ReadFile(pidFile) //nolint:gosec // we control the path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Check{
				Name:   name,
				State:  StateWarn,
				Detail: "not running (no pid file). Run `bitrise-build-cache xcelerate start-proxy` or `auth set` + `activate` first.",
			}
		}

		return Check{Name: name, State: StateError, Detail: "read pid file: " + err.Error()}
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return Check{Name: name, State: StateError, Detail: "pid file content invalid: " + err.Error()}
	}

	if err := syscall.Kill(pid, 0); err != nil {
		return Check{
			Name:   name,
			State:  StateError,
			Detail: fmt.Sprintf("pid %d in pid file but process not running. Stale pid file — remove %s or run start-proxy again.", pid, pidFile),
		}
	}

	return Check{Name: name, State: StateOK, Detail: fmt.Sprintf("running (pid %d)", pid)}
}

func (r *Runner) checkAuth() Check {
	const name = "auth"

	if r.AuthLoader != nil {
		creds, err := r.AuthLoader.Load()
		if err == nil && creds.AuthToken != "" && creds.WorkspaceID != "" {
			return Check{
				Name:   name,
				State:  StateOK,
				Detail: fmt.Sprintf("OS keychain, workspace=%s", creds.WorkspaceID),
			}
		}
	}

	if cfg, err := common.ReadAuthConfigFromEnvironments(r.Envs); err == nil {
		return Check{
			Name:   name,
			State:  StateOK,
			Detail: fmt.Sprintf("environment variables, workspace=%s", cfg.WorkspaceID),
		}
	}

	return Check{
		Name:   name,
		State:  StateError,
		Detail: "no credentials found. Run `bitrise-build-cache auth set --token … --workspace-id …` or set BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID.",
	}
}

func (r *Runner) checkCLIVersion(ctx context.Context) Check {
	const name = "cli-version"

	current := r.CLIVersion
	if current == "" {
		current = "devel"
	}

	// Local/devel builds are tagged with "+", "dirty" or "devel" and aren't
	// meaningful to compare against released tags.
	if isLocalBuild(current) {
		return Check{Name: name, State: StateOK, Detail: fmt.Sprintf("current=%s (local build)", current)}
	}

	latest, err := r.LatestReleaseTag(ctx, r.HTTPClient)
	if err != nil {
		return Check{
			Name:   name,
			State:  StateWarn,
			Detail: fmt.Sprintf("current=%s; could not check latest (%v)", current, err),
		}
	}
	if latest == "" {
		return Check{Name: name, State: StateOK, Detail: fmt.Sprintf("current=%s", current)}
	}

	if current == latest || strings.TrimPrefix(current, "v") == strings.TrimPrefix(latest, "v") {
		return Check{Name: name, State: StateOK, Detail: fmt.Sprintf("current=%s (up to date)", current)}
	}

	return Check{
		Name:   name,
		State:  StateWarn,
		Detail: fmt.Sprintf("current=%s, latest=%s — run `bitrise-build-cache update` or `brew upgrade`", current, latest),
	}
}

func isLocalBuild(version string) bool {
	return version == "devel" ||
		strings.Contains(version, "+") ||
		strings.Contains(version, "dirty")
}

// fetchLatestGitHubRelease returns the tag_name of the most recent CLI release
// on GitHub. Network errors / 404s are returned to the caller so the check
// renders as warn (not error).
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
		// Distinguish offline / DNS from generic transport errors.
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
