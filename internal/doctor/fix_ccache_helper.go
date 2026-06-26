package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

func (d *Doctor) ccacheHelperFix(socketPath string) func() (string, error) {
	return func() (string, error) {
		if _, err := os.Stat(socketPath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return d.daemonUpFix()
			}

			return "", fmt.Errorf("stat %s: %w", socketPath, err)
		}

		if err := os.Remove(socketPath); err != nil {
			return "", fmt.Errorf("remove %s: %w", socketPath, err)
		}

		return "removed orphan socket " + socketPath, nil
	}
}
