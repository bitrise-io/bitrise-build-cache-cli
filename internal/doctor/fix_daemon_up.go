package doctor

import (
	"context"
	"fmt"
	"strings"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
)

type DaemonUpFixer struct {
	Up func(ctx context.Context) ([]string, error)
}

//nolint:contextcheck // Fixer.Fix is ctx-less by design; Background is correct here.
func (f DaemonUpFixer) Fix() (string, error) {
	up := f.Up
	if up == nil {
		up = defaultDaemonUp
	}

	started, err := up(context.Background())
	if err != nil {
		return "", fmt.Errorf("daemon up: %w", err)
	}

	if len(started) == 0 {
		return "daemon up: no services to start", nil
	}

	return "started: " + strings.Join(started, ", "), nil
}

func defaultDaemonUp(ctx context.Context) ([]string, error) {
	backend, err := daemonpkg.DefaultBackend()
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel
	}

	paths, err := daemonpkg.NewPaths()
	if err != nil {
		return nil, err //nolint:wrapcheck // already context-rich
	}

	result, err := daemonpkg.Up(ctx, backend, paths, daemonpkg.DefaultServices())
	if err != nil {
		return nil, err //nolint:wrapcheck // already context-rich
	}

	started := make([]string, 0, len(result.Statuses))
	for _, st := range result.Statuses {
		started = append(started, st.Service.Name)
	}

	return started, nil
}
