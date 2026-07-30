//go:build unit

package tui

import (
	"errors"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every screen has to be leaveable the same way, and esc is the one users try
// first. ctrl+c stays bound because that is what huh ships with.
func TestKeyMap_AbortsOnEscAndCtrlC(t *testing.T) {
	km := KeyMap()

	keys := km.Quit.Keys()
	assert.Contains(t, keys, "esc")
	assert.Contains(t, keys, "ctrl+c")
	assert.Equal(t, "esc", km.Quit.Help().Key, "the help line should advertise esc")
}

func TestTranslateFormErr(t *testing.T) {
	assert.NoError(t, translateFormErr(nil))
	assert.ErrorIs(t, translateFormErr(huh.ErrUserAborted), ErrAborted)

	other := errors.New("boom")
	err := translateFormErr(other)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrAborted, "a real failure must not look like a cancellation")
	assert.ErrorIs(t, err, other, "the cause has to survive wrapping")
}

// The option viewport is sized as Height minus the title and description, so the
// allowance has to track the text or the list silently loses rows.
func TestChrome(t *testing.T) {
	assert.Equal(t, 2, Chrome("one line"))
	assert.Equal(t, 4, Chrome("one line\n\nand a note"))
}
