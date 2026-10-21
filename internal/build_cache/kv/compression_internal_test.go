//go:build unit

package kv

import (
	"bytes"
	"crypto/rand"
	"os/exec"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sizes straddle the raw block limit and the 1-byte and 2-byte content-size ranges of other encoders.
var rawFrameSizes = []int{0, 1, 99, 100, 255, 256, 65791, 65792, maxRawBlockSize - 1, maxRawBlockSize, maxRawBlockSize + 1, 3*maxRawBlockSize + 5} //nolint:gochecknoglobals

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()

	b := make([]byte, n)
	_, err := rand.Read(b)
	require.NoError(t, err)

	return b
}

func TestRawFrame_DecodesToTheInput(t *testing.T) {
	dec, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer dec.Close()

	for _, n := range rawFrameSizes {
		data := randomBytes(t, n)

		got, err := dec.DecodeAll(rawFrame(data), nil)

		require.NoError(t, err, "size %d", n)
		assert.True(t, bytes.Equal(data, got), "size %d", n)
	}
}

func TestRawFrame_CarriesTheDataUncompressed(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 4096)

	frame := rawFrame(data)

	assert.GreaterOrEqual(t, len(frame), len(data))
	assert.LessOrEqual(t, len(frame), len(data)+16)
}

// An independent implementation must accept the hand-built frame too; skipped where the zstd CLI is absent.
func TestRawFrame_AcceptedByTheZstdCLI(t *testing.T) {
	path, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("zstd CLI not installed")
	}

	for _, n := range rawFrameSizes {
		data := randomBytes(t, n)

		cmd := exec.Command(path, "-d", "-c") //nolint:gosec,noctx
		cmd.Stdin = bytes.NewReader(rawFrame(data))
		out, err := cmd.Output()

		require.NoError(t, err, "size %d", n)
		assert.True(t, bytes.Equal(data, out), "size %d", n)
	}
}
