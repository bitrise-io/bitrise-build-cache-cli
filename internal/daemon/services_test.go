//go:build unit

package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithDebugLogging_PrependsRootFlag(t *testing.T) {
	got := WithDebugLogging(DefaultServices())

	require.Len(t, got, len(DefaultServices()))
	for i, svc := range got {
		// --debug is a persistent root flag, so it must come before the
		// subcommand or cobra rejects it.
		assert.Equal(t, "--debug", svc.Args[0])
		assert.Equal(t, DefaultServices()[i].Args, svc.Args[1:])
	}
}

func TestWithDebugLogging_DoesNotMutateInput(t *testing.T) {
	in := DefaultServices()
	_ = WithDebugLogging(in)

	assert.Equal(t, DefaultServices(), in)
}
