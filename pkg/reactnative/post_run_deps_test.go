//go:build unit

package reactnative

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRNInvocationDetailsURL_WithWorkspace(t *testing.T) {
	got := rnInvocationDetailsURL("ws-slug", "inv-id")
	assert.Equal(t,
		"https://app-staging.bitrise.io/build-cache/ws-slug/invocations/react-native/inv-id",
		got,
	)
}
