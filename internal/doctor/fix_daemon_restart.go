package doctor

import (
	"context"
	"fmt"
	"strings"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

type DaemonRestartFixer struct {
	Restart func(ctx context.Context) ([]string, error)
}

//nolint:contextcheck // Fixer.Fix is ctx-less by design; Background is correct here.
func (f DaemonRestartFixer) Fix() (string, error) {
	restart := f.Restart
	if restart == nil {
		restart = defaultDaemonRestart
	}

	restarted, err := restart(context.Background())
	if err != nil {
		return "", fmt.Errorf("daemon restart: %w", err)
	}

	if len(restarted) == 0 {
		return "daemon restart: no services touched", nil
	}

	return "restarted: " + strings.Join(restarted, ", "), nil
}

func defaultDaemonRestart(ctx context.Context) ([]string, error) {
	backend, err := daemonpkg.DefaultBackend()
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel
	}

	paths, err := daemonpkg.NewPaths()
	if err != nil {
		return nil, err //nolint:wrapcheck // already context-rich
	}

	if _, err := daemonpkg.Down(ctx, backend, paths, daemonpkg.DefaultServices()); err != nil {
		return nil, err //nolint:wrapcheck // already context-rich
	}

	result, err := daemonpkg.Up(ctx, backend, paths, daemonpkg.DefaultServices())
	if err != nil {
		return nil, err //nolint:wrapcheck // already context-rich
	}

	restarted := make([]string, 0, len(result.Statuses))
	for _, st := range result.Statuses {
		restarted = append(restarted, st.Service.Name)
	}

	return restarted, nil
}
