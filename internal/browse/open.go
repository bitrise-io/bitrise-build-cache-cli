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

type Opener interface {
	Open(ctx context.Context, url string) error
}

type DefaultOpener struct {
	// CommandRunner is the injection point for tests. Nil = real exec.
	CommandRunner func(ctx context.Context, bin string, args ...string) error
}

// Open returns ErrNoOpener on unsupported platforms so callers can fall
// back to printing the URL — browse should not hard-fail just because we
// couldn't auto-launch a browser.
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

// launcherForOS picks the binary for the host. Linux + BSDs ship
// `xdg-open` (freedesktop standard); macOS uses `open`. Other platforms
// return ok=false so the caller picks the print-URL fallback rather than
// us guessing.
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
