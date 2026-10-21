//go:build unit

package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
)

func TestActivateXcode_CompressionFlagsDefaultOn(t *testing.T) {
	compression := activateXcodeCmd.Flags().Lookup("remote-cache-compression")
	require.NotNil(t, compression)
	assert.Equal(t, "true", compression.DefValue)

	threshold := activateXcodeCmd.Flags().Lookup("remote-cache-compression-threshold")
	require.NotNil(t, threshold)
	assert.Equal(t, "100", threshold.DefValue)
	assert.EqualValues(t, kv.DefaultCompressionThreshold, 100)
}
