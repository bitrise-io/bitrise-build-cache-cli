package doctor

import (
	"fmt"
	"os"
	"strings"
)

type LogDirsFixer struct {
	Candidates []string
}

func (f LogDirsFixer) Fix() (string, error) {
	created := make([]string, 0, len(f.Candidates))

	for _, path := range f.Candidates {
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
