package common

import (
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
)

// selectHeight caps how many options a picker renders at once. huh only applies
// its own default to dynamic OptionsFunc fields, so a static Options() list is
// rendered in full — an 81-workspace account repainted an 82-line frame on every
// keypress. Capping it keeps the frame (and the redraw) constant.
const selectHeight = 12

// ErrAborted reports that the user cancelled an interactive prompt. Callers
// report it and stop, rather than treating it as a failure.
var ErrAborted = errors.New("cancelled by user")

// interactiveKeyMap is huh's default map with esc added alongside ctrl+c as an
// abort, so every screen can be left the same way. Safe because esc's other
// bindings (set/clear filter) are only enabled while filtering is on, which none
// of our fields turn on.
func interactiveKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))

	return km
}

// runForm runs the groups as one abortable form, translating huh's abort into
// ErrAborted so callers don't have to know about huh.
func runForm(groups ...*huh.Group) error {
	err := huh.NewForm(groups...).WithKeyMap(interactiveKeyMap()).Run()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, huh.ErrUserAborted):
		return ErrAborted
	default:
		return fmt.Errorf("interactive wizard: %w", err)
	}
}
