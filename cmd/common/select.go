package common

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// selectFromList shows a huh single-select and returns the chosen 0-based index.
// Requires an interactive terminal (huh's requirement, matching
// `activate --interactive`); callers gate on a TTY before reaching here.
func selectFromList(prompt string, items []string) (int, error) {
	options := make([]huh.Option[int], len(items))
	for i, item := range items {
		options[i] = huh.NewOption(item, i)
	}

	choice := 0
	if err := huh.NewSelect[int]().
		Title(prompt).
		Options(options...).
		Value(&choice).
		Run(); err != nil {
		return 0, fmt.Errorf("interactive selection: %w", err)
	}

	return choice, nil
}
