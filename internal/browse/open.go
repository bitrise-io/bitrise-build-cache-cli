package browse

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

var ErrNoOpener = errors.New("no supported browser opener for this OS")

type Opener interface {
	Open(ctx context.Context, url string) error
}

type DefaultOpener struct {
	CommandRunner func(ctx context.Context, bin string, args ...string) error
}

// Open returns ErrNoOpener rather than failing hard — the caller can fall back to printing.
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
