package doctor

import (
	"fmt"
	"os"
)

type RemoveFileFixer struct {
	Path  string
	Label string
}

func (f RemoveFileFixer) Fix() (string, error) {
	if err := os.Remove(f.Path); err != nil {
		return "", fmt.Errorf("remove %s: %w", f.Path, err)
	}

	return "removed " + f.Label + " " + f.Path, nil
}
