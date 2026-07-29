package common

import (
	"errors"
	"fmt"

	"charm.land/huh/v2"
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
	err := huh.NewSelect[int]().
		Title(prompt).
		Options(options...).
		Height(selectHeight).
		Value(&choice).
		WithKeyMap(interactiveKeyMap()).
		Run()
	switch {
	case err == nil:
		return choice, nil
	case errors.Is(err, huh.ErrUserAborted):
		return 0, ErrAborted
	default:
		return 0, fmt.Errorf("interactive selection: %w", err)
	}
}
