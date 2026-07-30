// Package tui holds the shared behaviour of the CLI's interactive forms, so
// every screen aborts the same way and sizes its lists the same way.
package tui

import (
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
)

// ErrAborted reports that the user cancelled a prompt. Callers report it and
// stop rather than treating it as a failure.
var ErrAborted = errors.New("cancelled by user")

// SelectHeight caps how many options a picker renders at once. huh applies its
// own default only to dynamic OptionsFunc fields, so a static Options() list is
// otherwise rendered in full — an 81-workspace account repainted an 82-line
// frame per keypress. Capping makes the frame constant; the redraw stays linear
// in the option count, because optionsView renders them all before the viewport
// trims.
const SelectHeight = 12

// SelectWidth is set because huh derives maxWidth from the field width, and an
// unset one leaves it negative.
const SelectWidth = 72

// Chrome is the number of lines huh subtracts from a field's Height to size its
// option viewport: one title plus however many the description occupies. Without
// allowing for them, a 4-option list with a title and a one-line description
// renders only 2 rows.
func Chrome(description string) int {
	lines := 1
	for _, r := range description {
		if r == '\n' {
			lines++
		}
	}

	return 1 + lines
}

// KeyMap is huh's default map with esc added alongside ctrl+c as an abort, so
// every screen can be left the same way. Safe because esc's other huh bindings
// (set/clear filter) are only enabled while filtering is on, which none of our
// fields turn on.
func KeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))

	return km
}

// RunForm runs the groups as one abortable form, translating huh's abort into
// ErrAborted so callers don't have to know about huh.
func RunForm(groups ...*huh.Group) error {
	err := huh.NewForm(groups...).WithKeyMap(KeyMap()).Run()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, huh.ErrUserAborted):
		return ErrAborted
	default:
		return fmt.Errorf("interactive form: %w", err)
	}
}
