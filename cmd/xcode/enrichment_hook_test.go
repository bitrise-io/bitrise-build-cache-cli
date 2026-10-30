//go:build unit

package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProvisionalDerivedDataPathFromManifest_grandparentOfManifestDir(t *testing.T) {
	// Standard IDE layout: …/<DD>/Logs/Build/LogStoreManifest.plist
	got := provisionalDerivedDataPathFromManifest(
		"/Users/me/Library/Developer/Xcode/DerivedData/App-abc/Logs/Build/LogStoreManifest.plist",
	)
	assert.Equal(t, "/Users/me/Library/Developer/Xcode/DerivedData/App-abc", got)
}

func TestProvisionalDerivedDataPathFromManifest_emptyPath(t *testing.T) {
	assert.Empty(t, provisionalDerivedDataPathFromManifest(""))
}
