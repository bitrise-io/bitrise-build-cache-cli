//go:build unit

package xcode_app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultXcodeChecker_returnsPIDsOnMatch(t *testing.T) {
	r := &fakeRunner{stdout: "1234\n5678\n", exit: 0}
	c := DefaultXcodeChecker{Runner: r}

	pids, err := c.RunningPIDs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []int{1234, 5678}, pids)
}

func TestDefaultXcodeChecker_exit1MeansNotRunning(t *testing.T) {
	r := &fakeRunner{exit: 1}
	c := DefaultXcodeChecker{Runner: r}

	pids, err := c.RunningPIDs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pids)
}

func TestDefaultXcodeChecker_execErrorPropagates(t *testing.T) {
	r := &fakeRunner{err: errors.New("pgrep missing")}
	c := DefaultXcodeChecker{Runner: r}

	_, err := c.RunningPIDs(context.Background())
	require.Error(t, err)
}

func TestParsePIDs_filtersInvalidLines(t *testing.T) {
	assert.Equal(t, []int{1, 2}, parsePIDs("1\nnot-a-pid\n\n2\n"))
}

func TestParsePIDs_emptyStringReturnsEmpty(t *testing.T) {
	assert.Empty(t, parsePIDs(""))
}
