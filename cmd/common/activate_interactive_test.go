//go:build unit

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivateCmd_HasInteractiveFlag(t *testing.T) {
	flag := ActivateCmd.Flags().Lookup("interactive")
	require.NotNil(t, flag, "--interactive flag should be registered on activate command")
	assert.Equal(t, "false", flag.DefValue)
}
