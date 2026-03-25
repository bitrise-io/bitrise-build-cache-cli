package ccache

// buildStorageHelperCallbacks constructs the onChildInvocation callback for the IPC server.
//
// The storage helper is only responsible for proxying remote storage and registering
// invocation relations. Ccache stat collection and zeroing is handled separately
// by the collect-stats command or a wrapper post-run action.
//
// registerFn is injectable to allow unit testing without hitting the network.
func buildStorageHelperCallbacks(
	registerFn func(parentID, childID string),
) func(prevID, parentID, childID string, dl, ul int64) {
	return func(_, parentID, childID string, _, _ int64) {
		registerFn(parentID, childID)
	}
}
