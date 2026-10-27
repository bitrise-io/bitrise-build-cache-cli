//go:build unit

package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToNames_ProjectsNamesInOrder(t *testing.T) {
	in := []AncestryEntry{
		{PID: 1, Name: "swiftc", StartTimeMS: 1},
		{PID: 2, Name: "xcodebuild", StartTimeMS: 2},
	}

	assert.Equal(t, []string{"swiftc", "xcodebuild"}, toNames(in))
	assert.Nil(t, toNames(nil))
}
