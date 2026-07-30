package interactive

import (
	"charm.land/huh/v2"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
)

// ErrAborted is re-exported so callers in this package keep one name for it.
var ErrAborted = tui.ErrAborted

const (
	selectHeight = tui.SelectHeight
	selectWidth  = tui.SelectWidth
)

func selectChrome(description string) int { return tui.Chrome(description) }

func interactiveKeyMap() *huh.KeyMap { return tui.KeyMap() }

func runForm(groups ...*huh.Group) error {
	return tui.RunForm(groups...) //nolint:wrapcheck // tui returns ErrAborted or an already-wrapped error
}
