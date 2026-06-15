package browse

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// ErrNoOpener is returned by DefaultOpener.Open when the host OS doesn't
// have a recognised default-browser launcher (Windows / unknown). Callers
// fall back to printing the URL.
var ErrNoOpener = errors.New("no supported browser opener for this OS")

// Opener launches the user's default browser at the given URL. Production
// uses DefaultOpener (exec macOS `open` / Linux `xdg-open`); tests inject
// a stub that records the URL without actually spawning anything.
type Opener interface {
	Open(ctx context.Context, url string) error
}

// DefaultOpener runs the platform's default browser-launcher binary.
// CommandRunner is injectable for tests; nil falls back to a stdlib
// exec.CommandContext implementation.
type DefaultOpener struct {
	// CommandRunner runs the launcher binary. Returning a wrapped error
	// surfaces "binary not found" / "exec failed" without the caller
	// having to know which launcher we tried.
	CommandRunner func(ctx context.Context, bin string, args ...string) error
}

// Open executes the platform-specific browser-launcher binary against url.
// Returns ErrNoOpener on unsupported platforms so the caller can fall back
// to printing the URL — the browse subcommand should not be a hard failure
// just because we couldn't auto-launch a browser.
func (o DefaultOpener) Open(ctx context.Context, url string) error {
	bin, args, ok := launcherForOS(runtime.GOOS, url)
	if !ok {
		return ErrNoOpener
	}

	runner := o.CommandRunner
	if runner == nil {
		runner = defaultRunner
	}

	if err := runner(ctx, bin, args...); err != nil {
		return fmt.Errorf("%s %s: %w", bin, url, err)
	}

	return nil
}

// launcherForOS picks the binary + args for the host. Linux + BSDs ship
// `xdg-open` as the freedesktop-standard launcher; macOS uses `open`. Other
// platforms (Windows, unknown) return ok=false so the caller picks the
// print-URL fallback rather than us guessing.
func launcherForOS(goos, url string) (string, []string, bool) {
	switch goos {
	case "darwin":
		return "/usr/bin/open", []string{url}, true
	case "linux", "freebsd", "openbsd", "netbsd":
		return "xdg-open", []string{url}, true
	default:
		return "", nil, false
	}
}

func defaultRunner(ctx context.Context, bin string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}
