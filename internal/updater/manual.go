package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/logio"
)

const InstallerURL = "https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh"

const DownloadTimeout = 10 * time.Second

// MaxInstallerBytes — the real script is <100 KiB; 1 MiB is a wide safety margin against hostile origins.
const MaxInstallerBytes = 1 << 20

type ManualOptions struct {
	Bindir       string
	Logger       log.Logger
	InstallerURL string
	HTTPClient   *http.Client
	Shell        string
	DryRun       bool
}

func ManualUpgrade(ctx context.Context, opts ManualOptions) (string, error) {
	if opts.Bindir == "" {
		return "", errors.New("bindir is required for manual upgrade")
	}

	if opts.Logger == nil {
		return "", errors.New("logger is required for manual upgrade")
	}

	if opts.InstallerURL == "" {
		opts.InstallerURL = InstallerURL
	}

	if opts.Shell == "" {
		opts.Shell = "/bin/sh"
	}

	scriptPath, err := downloadInstaller(ctx, opts.HTTPClient, opts.InstallerURL)
	if err != nil {
		return "", err
	}

	if opts.DryRun {
		opts.Logger.Infof("Dry run — would run: %s %s -b %s", opts.Shell, scriptPath, opts.Bindir)
		opts.Logger.Infof("Rerun without --dry-run to apply.")

		if removeErr := os.Remove(scriptPath); removeErr != nil {
			opts.Logger.Warnf("could not clean up dry-run installer temp file %s: %s", scriptPath, removeErr)
		}

		return "", nil
	}

	opts.Logger.Infof("Running installer to upgrade CLI in %s ...", opts.Bindir)

	pipe := logio.NewWriter(opts.Logger)
	defer pipe.Flush()

	cmd := exec.CommandContext(ctx, opts.Shell, scriptPath, "-b", opts.Bindir) //nolint:gosec // scriptPath is our temp file we just wrote
	cmd.Stdout = pipe
	cmd.Stderr = pipe

	if err := cmd.Run(); err != nil {
		// Script stays on disk on failure so the user can re-run it manually to debug.
		return scriptPath, fmt.Errorf("run installer.sh: %w", err)
	}

	if removeErr := os.Remove(scriptPath); removeErr != nil {
		opts.Logger.Warnf("could not clean up installer temp file %s: %s", scriptPath, removeErr)
	}

	opts.Logger.Donef("Upgrade complete.")

	return "", nil
}

func downloadInstaller(ctx context.Context, client *http.Client, url string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: DownloadTimeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build installer request: %w", err)
	}

	req.Header.Set("User-Agent", "bitrise-build-cache-cli/update")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download installer.sh: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	// Strict 2xx — a 3xx HTML body that Go's client failed to follow would otherwise be exec'd as a shell script.
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("download installer.sh: server responded %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "bitrise-build-cache-installer-*.sh")
	if err != nil {
		return "", fmt.Errorf("create installer temp file: %w", err)
	}

	// Cap+1 byte lets us detect cap-hit via n > MaxInstallerBytes below.
	limited := io.LimitReader(resp.Body, MaxInstallerBytes+1)

	n, err := io.Copy(tmp, limited)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		return "", fmt.Errorf("write installer to temp: %w", err)
	}

	if n > MaxInstallerBytes {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		return "", fmt.Errorf("installer.sh exceeds %d bytes — refusing to execute oversized script", MaxInstallerBytes)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())

		return "", fmt.Errorf("close installer temp file: %w", err)
	}

	if err := os.Chmod(tmp.Name(), 0o700); err != nil { //nolint:gosec // intentional: script must be readable + executable
		_ = os.Remove(tmp.Name())

		return "", fmt.Errorf("chmod installer temp: %w", err)
	}

	return tmp.Name(), nil
}
