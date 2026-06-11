package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// InstallerURL is the canonical location of installer.sh. The file at this
// URL is the same one shipped at install/installer.sh in this repo —
// reusing it instead of duplicating download / GAR-fallback / checksum logic
// keeps the update flow in lockstep with the install flow.
const InstallerURL = "https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh"

// DownloadTimeout caps the installer.sh fetch. The script itself is small
// (<10 KB); a short timeout makes a network blip surface as an error fast.
const DownloadTimeout = 10 * time.Second

// ManualOptions bundles the inputs ManualUpgrade needs. Kept as a struct so
// tests can override URL / HTTP client / shell / output writer.
type ManualOptions struct {
	// Bindir is the directory containing the running binary — passed to
	// installer.sh via `-b`. Required.
	Bindir string
	// Out is where progress / completion messages go. Stderr in production
	// (keeps stdout clean for JSON consumers).
	Out io.Writer
	// InstallerURL overrides the canonical URL for tests. Empty = use
	// InstallerURL constant.
	InstallerURL string
	// HTTPClient is the network client. Empty = default 10s-timeout client.
	HTTPClient *http.Client
	// Shell is the program to invoke installer.sh with. Empty = "/bin/sh".
	Shell string
	// DryRun, when true, downloads the installer but doesn't execute it.
	// Returns the path of the downloaded script so the caller can show it.
	DryRun bool
}

// ManualUpgrade downloads installer.sh and runs it against the bindir of the
// running binary. installer.sh handles tarball download, checksum
// verification, and atomic replacement of the binary internally — we just
// drive it.
//
// Returns the local path of the downloaded installer (useful for diagnostics
// and the DryRun case). The file is left on disk under os.TempDir() so a
// later debug pass can re-run it; it's tiny.
func ManualUpgrade(ctx context.Context, opts ManualOptions) (string, error) {
	if opts.Bindir == "" {
		return "", errors.New("bindir is required for manual upgrade")
	}

	if opts.Out == nil {
		opts.Out = os.Stderr
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
		fmt.Fprintf(opts.Out, "Dry run — installer downloaded to %s but NOT executed.\n", scriptPath)
		fmt.Fprintf(opts.Out, "To upgrade manually: %s %s -b %s\n", opts.Shell, scriptPath, opts.Bindir)

		return scriptPath, nil
	}

	fmt.Fprintf(opts.Out, "Running installer to upgrade CLI in %s ...\n", opts.Bindir)

	cmd := exec.CommandContext(ctx, opts.Shell, scriptPath, "-b", opts.Bindir) //nolint:gosec // scriptPath is our temp file we just wrote
	cmd.Stdout = opts.Out
	cmd.Stderr = opts.Out

	if err := cmd.Run(); err != nil {
		return scriptPath, fmt.Errorf("run installer.sh: %w", err)
	}

	fmt.Fprintln(opts.Out, "Upgrade complete.")

	return scriptPath, nil
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

	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("download installer.sh: server responded %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "bitrise-build-cache-installer-*.sh")
	if err != nil {
		return "", fmt.Errorf("create installer temp file: %w", err)
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		return "", fmt.Errorf("write installer to temp: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())

		return "", fmt.Errorf("close installer temp file: %w", err)
	}

	// installer.sh is sh-piped in production; making the file executable
	// keeps the local invocation form available too if a future caller wants
	// to skip the explicit shell.
	if err := os.Chmod(tmp.Name(), 0o700); err != nil { //nolint:gosec // intentional: script must be readable + executable
		return "", fmt.Errorf("chmod installer temp: %w", err)
	}

	return tmp.Name(), nil
}

// BindirOf returns the directory of the supplied executable path — the value
// to pass installer.sh's -b flag so the upgrade lands in the same spot.
func BindirOf(executable string) string {
	return filepath.Dir(executable)
}
