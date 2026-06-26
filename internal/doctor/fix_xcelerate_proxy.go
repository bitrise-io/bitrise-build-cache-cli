package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func (d *Doctor) xcelerateProxyFix(pidPath string) func() (string, error) {
	return func() (string, error) {
		content, err := os.ReadFile(pidPath) //nolint:gosec // path resolved via xceleratconfig helper
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return d.daemonUpFix()
			}

			return "", fmt.Errorf("read %s: %w", pidPath, err)
		}

		pid, perr := strconv.Atoi(strings.TrimSpace(string(content)))
		if perr != nil {
			if err := os.Remove(pidPath); err != nil {
				return "", fmt.Errorf("remove %s: %w", pidPath, err)
			}

			return "removed corrupt " + pidPath, nil
		}

		if err := syscall.Kill(pid, 0); err != nil {
			if err := os.Remove(pidPath); err != nil {
				return "", fmt.Errorf("remove %s: %w", pidPath, err)
			}

			return "removed stale " + pidPath, nil
		}

		return d.daemonRestartFix()
	}
}
