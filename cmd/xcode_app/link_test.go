//go:build unit

package xcode_app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkCmd_isRegisteredUnderXcodeApp(t *testing.T) {
	link, _, err := xcodeAppCmd.Find([]string{"link"})
	require.NoError(t, err)
	assert.Equal(t, "link <path>", link.Use)

	unlink, _, err := xcodeAppCmd.Find([]string{"unlink"})
	require.NoError(t, err)
	assert.Equal(t, "unlink <path>", unlink.Use)
}

func TestLinkCmd_requiresExactlyOneArg(t *testing.T) {
	err := linkCmd.Args(linkCmd, nil)
	assert.Error(t, err)

	err = linkCmd.Args(linkCmd, []string{"a", "b"})
	assert.Error(t, err)
}

func TestUnlinkCmd_requiresExactlyOneArg(t *testing.T) {
	err := unlinkCmd.Args(unlinkCmd, nil)
	assert.Error(t, err)

	err = unlinkCmd.Args(unlinkCmd, []string{"a", "b"})
	assert.Error(t, err)
}
