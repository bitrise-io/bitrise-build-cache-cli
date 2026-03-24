//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type statsCall struct {
	invocationID string
	parentID     string
	dl           int64
	ul           int64
}

type registerCall struct {
	parentID string
	childID  string
}

func Test_buildStorageHelperCallbacks_no_SetInvocationID_shutdown_uses_initial_parent(t *testing.T) {
	var statsCalls []statsCall

	_, onShutdown := buildStorageHelperCallbacks(
		"env-parent",
		func(_, _ string) {},
		func(invID, parentID string, dl, ul int64) {
			statsCalls = append(statsCalls, statsCall{invID, parentID, dl, ul})
		},
		func() {},
	)

	onShutdown("initial-id", 10, 20)

	require.Len(t, statsCalls, 1)
	assert.Equal(t, "initial-id", statsCalls[0].invocationID)
	assert.Equal(t, "env-parent", statsCalls[0].parentID)
	assert.Equal(t, int64(10), statsCalls[0].dl)
	assert.Equal(t, int64(20), statsCalls[0].ul)
}

func Test_buildStorageHelperCallbacks_first_child_uses_initial_parent(t *testing.T) {
	var statsCalls []statsCall

	onChild, _ := buildStorageHelperCallbacks(
		"env-parent",
		func(_, _ string) {},
		func(invID, parentID string, dl, ul int64) {
			statsCalls = append(statsCalls, statsCall{invID, parentID, dl, ul})
		},
		func() {},
	)

	// First SetInvocationID: prevID is initial-id, its parent is env-parent
	onChild("initial-id", "react-native-run-1", "ccache-child-1", 100, 50)

	require.Len(t, statsCalls, 1)
	assert.Equal(t, "initial-id", statsCalls[0].invocationID)
	assert.Equal(t, "env-parent", statsCalls[0].parentID, "first child's prevID parent should be the initial env parent")
}

func Test_buildStorageHelperCallbacks_second_child_uses_first_parentID(t *testing.T) {
	var statsCalls []statsCall

	onChild, _ := buildStorageHelperCallbacks(
		"env-parent",
		func(_, _ string) {},
		func(invID, parentID string, dl, ul int64) {
			statsCalls = append(statsCalls, statsCall{invID, parentID, dl, ul})
		},
		func() {},
	)

	onChild("initial-id", "react-native-run-1", "ccache-child-1", 0, 0)
	onChild("ccache-child-1", "react-native-run-2", "ccache-child-2", 200, 80)

	require.Len(t, statsCalls, 2)
	// Second call: prevID is ccache-child-1, whose parent was react-native-run-1
	assert.Equal(t, "ccache-child-1", statsCalls[1].invocationID)
	assert.Equal(t, "react-native-run-1", statsCalls[1].parentID, "second prevID parent should be the first call's parentID")
}

func Test_buildStorageHelperCallbacks_shutdown_after_children_uses_last_parentID(t *testing.T) {
	var statsCalls []statsCall

	onChild, onShutdown := buildStorageHelperCallbacks(
		"env-parent",
		func(_, _ string) {},
		func(invID, parentID string, dl, ul int64) {
			statsCalls = append(statsCalls, statsCall{invID, parentID, dl, ul})
		},
		func() {},
	)

	onChild("initial-id", "react-native-run-1", "ccache-child-1", 0, 0)
	onChild("ccache-child-1", "react-native-run-2", "ccache-child-2", 0, 0)
	onShutdown("ccache-child-2", 300, 100)

	require.Len(t, statsCalls, 3)
	shutdownCall := statsCalls[2]
	assert.Equal(t, "ccache-child-2", shutdownCall.invocationID)
	assert.Equal(t, "react-native-run-2", shutdownCall.parentID, "shutdown activeID parent should be the last SetInvocationID's parentID")
	assert.Equal(t, int64(300), shutdownCall.dl)
	assert.Equal(t, int64(100), shutdownCall.ul)
}

func Test_buildStorageHelperCallbacks_register_called_with_new_child(t *testing.T) {
	var registerCalls []registerCall

	onChild, _ := buildStorageHelperCallbacks(
		"env-parent",
		func(parentID, childID string) {
			registerCalls = append(registerCalls, registerCall{parentID, childID})
		},
		func(_, _ string, _, _ int64) {},
		func() {},
	)

	onChild("initial-id", "react-native-run-1", "ccache-child-1", 0, 0)
	onChild("ccache-child-1", "react-native-run-2", "ccache-child-2", 0, 0)

	require.Len(t, registerCalls, 2)
	assert.Equal(t, registerCall{"react-native-run-1", "ccache-child-1"}, registerCalls[0])
	assert.Equal(t, registerCall{"react-native-run-2", "ccache-child-2"}, registerCalls[1])
}

func Test_buildStorageHelperCallbacks_zero_called_after_each_child(t *testing.T) {
	var zeroCalls int

	onChild, _ := buildStorageHelperCallbacks(
		"env-parent",
		func(_, _ string) {},
		func(_, _ string, _, _ int64) {},
		func() { zeroCalls++ },
	)

	onChild("initial-id", "p1", "c1", 0, 0)
	onChild("c1", "p2", "c2", 0, 0)

	assert.Equal(t, 2, zeroCalls)
}
