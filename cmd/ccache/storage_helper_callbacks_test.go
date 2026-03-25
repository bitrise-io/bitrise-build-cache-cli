//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type registerCall struct {
	parentID string
	childID  string
}

func Test_buildStorageHelperCallbacks_register_called_with_new_child(t *testing.T) {
	var registerCalls []registerCall

	onChild := buildStorageHelperCallbacks(
		func(parentID, childID string) {
			registerCalls = append(registerCalls, registerCall{parentID, childID})
		},
	)

	onChild("initial-id", "react-native-run-1", "ccache-child-1", 0, 0)
	onChild("ccache-child-1", "react-native-run-2", "ccache-child-2", 0, 0)

	require.Len(t, registerCalls, 2)
	assert.Equal(t, registerCall{"react-native-run-1", "ccache-child-1"}, registerCalls[0])
	assert.Equal(t, registerCall{"react-native-run-2", "ccache-child-2"}, registerCalls[1])
}

func Test_buildStorageHelperCallbacks_register_not_called_when_no_invocations(t *testing.T) {
	var registerCalls []registerCall

	_ = buildStorageHelperCallbacks(
		func(parentID, childID string) {
			registerCalls = append(registerCalls, registerCall{parentID, childID})
		},
	)

	assert.Empty(t, registerCalls)
}
