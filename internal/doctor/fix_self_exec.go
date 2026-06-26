package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

func defaultRunSelf(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate cli executable: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), exe, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("re-exec %v: %w", args, err)
	}

	return nil
}

func (d *Doctor) runSelf(args ...string) error {
	runner := d.RunSelf
	if runner == nil {
		runner = defaultRunSelf
	}

	return runner(args...)
}

//nolint:contextcheck // Check.Fix is ctx-less by design; Background is correct here.
func (d *Doctor) daemonUpFix() (string, error) {
	up := d.DaemonUp
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
		return nil, err //nolint:wrapcheck // sentinel error from internal/daemon
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

//nolint:contextcheck // Check.Fix is ctx-less by design; Background is correct here.
func (d *Doctor) daemonRestartFix() (string, error) {
	restart := d.DaemonRestart
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

func (d *Doctor) updateFix() (string, error) {
	if err := d.runSelf("update"); err != nil {
		return "", err
	}

	return "ran `bitrise-build-cache update`", nil
}
