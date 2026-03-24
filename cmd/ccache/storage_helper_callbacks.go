package ccache

import "sync"

// buildStorageHelperCallbacks constructs the onChildInvocation and onShutdown callbacks
// for the IPC server with correct lastParentID tracking.
//
// parentInvocationID is the parent of the initial (pre-first-child) invocation.
// It advances to the parentID of each new child on every SetInvocationID, so that
// stats for the previous active invocation are always attributed to its own parent,
// not to the parent of the next incoming child.
//
// registerFn, collectFn, and zeroFn are injectable to allow unit testing without
// hitting the network or requiring ccache on PATH.
func buildStorageHelperCallbacks(
	parentInvocationID string,
	registerFn func(parentID, childID string),
	collectFn func(invocationID, parentID string, dl, ul int64),
	zeroFn func(),
) (func(prevID, parentID, childID string, dl, ul int64), func(activeID string, dl, ul int64)) {
	// lastParentID tracks the parent of the currently active invocation.
	// It starts as parentInvocationID and is updated on each SetInvocationID.
	lastParentID := parentInvocationID
	var mu sync.Mutex

	onChild := func(prevID, parentID, childID string, dl, ul int64) {
		mu.Lock()
		prevParentID := lastParentID
		lastParentID = parentID
		mu.Unlock()

		registerFn(parentID, childID)
		collectFn(prevID, prevParentID, dl, ul)
		zeroFn()
	}

	onShutdown := func(activeID string, dl, ul int64) {
		mu.Lock()
		activeParentID := lastParentID
		mu.Unlock()

		collectFn(activeID, activeParentID, dl, ul)
	}

	return onChild, onShutdown
}
