package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

// InstallerURL is the canonical location of installer.sh. The file at this
// URL is the same one shipped at install/installer.sh in this repo —
// reusing it instead of duplicating download / GAR-fallback / checksum logic
// keeps the update flow in lockstep with the install flow.
const InstallerURL = "https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh"

// DownloadTimeout caps the installer.sh fetch. The script itself is small
// (<10 KB); a short timeout makes a network blip surface as an error fast.
const DownloadTimeout = 10 * time.Second

// MaxInstallerBytes caps how much we'll read from the installer URL. The
// real script is well under 100 KiB — 1 MiB is two orders of magnitude
// safety margin while still bounding the worst case if a hostile / broken
// origin streams gigabytes into os.TempDir.
const MaxInstallerBytes = 1 << 20

// ManualOptions bundles the inputs ManualUpgrade needs. Kept as a struct so
// tests can override URL / HTTP client / shell / logger.
type ManualOptions struct {
	// Bindir is the directory containing the running binary — passed to
	// installer.sh via `-b`. Required.
	Bindir string
	// Logger receives our own progress / completion messages AND the
	// subprocess stdout/stderr (line-buffered via the loggerWriter adapter
	// below). Tests pass log.NewLogger(log.WithOutput(&buf)); production
	// passes a stderr-backed logger.
	Logger log.Logger
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
// and the DryRun case).
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
		// Dry run intentionally leaves the script on disk — the printed
		// "To upgrade manually" command references it, so removing it would
		// break copy-paste. Caller / user cleans up when done.
		opts.Logger.Infof("Dry run — installer downloaded to %s but NOT executed.", scriptPath)
		opts.Logger.Infof("To upgrade manually: %s %s -b %s", opts.Shell, scriptPath, opts.Bindir)

		return scriptPath, nil
	}

	opts.Logger.Infof("Running installer to upgrade CLI in %s ...", opts.Bindir)

	// loggerWriter buffers subprocess output until newlines so logger.Printf
	// emits one line per installer.sh log entry, preserving the user-visible
	// progress stream while flowing through the same logger.
	pipe := newLoggerWriter(opts.Logger)
	defer pipe.Flush()

	cmd := exec.CommandContext(ctx, opts.Shell, scriptPath, "-b", opts.Bindir) //nolint:gosec // scriptPath is our temp file we just wrote
	cmd.Stdout = pipe
	cmd.Stderr = pipe

	if err := cmd.Run(); err != nil {
		// Keep the script on disk on failure so the user can re-run it
		// manually with the same args to debug what went wrong.
		return scriptPath, fmt.Errorf("run installer.sh: %w", err)
	}

	// Success — drop the temp file. Best-effort; if removal fails the OS will
	// clean it up on next reboot (os.TempDir).
	if removeErr := os.Remove(scriptPath); removeErr != nil {
		opts.Logger.Warnf("could not clean up installer temp file %s: %s", scriptPath, removeErr)
	}

	opts.Logger.Donef("Upgrade complete.")

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

	// Strict 2xx range. Status / 100 == 2 (used previously) would silently
	// accept a 3xx redirect Go's client failed to follow — that produces an
	// HTML body that would go on to be exec'd as a shell script.
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("download installer.sh: server responded %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "bitrise-build-cache-installer-*.sh")
	if err != nil {
		return "", fmt.Errorf("create installer temp file: %w", err)
	}

	// LimitReader caps the body at MaxInstallerBytes so a hostile / broken
	// origin can't stream gigabytes into os.TempDir. We additionally check
	// whether the cap was actually hit (n == cap+1) by reading one extra byte.
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
		return "", fmt.Errorf("chmod installer temp: %w", err)
	}

	return tmp.Name(), nil
}

// BindirOf returns the directory of the supplied executable path — the value
// to pass installer.sh's -b flag so the upgrade lands in the same spot.
func BindirOf(executable string) string {
	return filepath.Dir(executable)
}

// loggerWriter is an io.Writer that line-buffers its input and emits each
// complete line via logger.Printf. Used to plumb subprocess stdout/stderr
// through the project logger — exec.Cmd needs an io.Writer but the rest of
// the package uses log.Logger.
type loggerWriter struct {
	logger log.Logger
	buf    bytes.Buffer
}

func newLoggerWriter(logger log.Logger) *loggerWriter {
	return &loggerWriter{logger: logger}
}

func (w *loggerWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)

	for {
		line, err := w.buf.ReadString('\n')
		if errors.Is(err, io.EOF) {
			// Incomplete trailing line — buffer it and wait for more.
			w.buf.WriteString(line)

			break
		}

		// Strip trailing newline; logger.Printf adds its own.
		w.logger.Printf("%s", trimNewline(line))
	}

	return len(p), nil
}

// Flush emits any remaining buffered partial line. Call after the
// subprocess exits to avoid losing a no-trailing-newline final line.
func (w *loggerWriter) Flush() {
	if w.buf.Len() == 0 {
		return
	}

	w.logger.Printf("%s", trimNewline(w.buf.String()))
	w.buf.Reset()
}

// trimNewline drops one trailing \n if present. Avoids importing strings
// for a single-char trim on the line buffer hot path.
func trimNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}

	return s
}
