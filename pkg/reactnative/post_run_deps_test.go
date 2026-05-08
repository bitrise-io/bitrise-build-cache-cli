//go:build unit

package reactnative

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRNInvocationDetailsURL_WithWorkspace(t *testing.T) {
	got := rnInvocationDetailsURL("ws-slug", "inv-id")
	assert.Equal(t,
		"https://app.bitrise.io/build-cache/ws-slug/invocations/react-native/inv-id",
		got,
	)
}

func TestRNInvocationDetailsURL_FallsBackWhenWorkspaceMissing(t *testing.T) {
	// The workspace-less URL does not currently render for react-native (the
	// reason ACI-4923 was filed), but the CLI must still emit *some* URL
	// when the workspace slug is unavailable, matching the other tools'
	// log lines and the BE behaviour expected once the public route is fixed.
	got := rnInvocationDetailsURL("", "inv-id")
	assert.Equal(t,
		"https://app.bitrise.io/build-cache/invocations/react-native/inv-id",
		got,
	)
}
