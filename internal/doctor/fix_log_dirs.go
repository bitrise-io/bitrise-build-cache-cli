package doctor

import (
	"fmt"
	"os"
	"strings"
)

func (d *Doctor) logDirsFix() (string, error) {
	created := []string{}
	for _, path := range d.StateDirCandidates {
		if _, err := os.Stat(path); err == nil {
			continue
		}

		if err := os.MkdirAll(path, 0o755); err != nil { //nolint:gosec
			return "", fmt.Errorf("mkdir %s: %w", path, err)
		}

		created = append(created, path)
	}

	return "created: " + strings.Join(created, ", "), nil
}
