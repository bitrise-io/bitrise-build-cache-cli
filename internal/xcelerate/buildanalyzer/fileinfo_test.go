//go:build unit

package buildanalyzer

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture: ImageTask.swift Node Value BLOB captured from a real Nuke
// snapshot post-mutation (+22 bytes to the file). Matches the layout
// hypothesis: kind=0x02, exists=1, mode=0x81A4, size=21121.
const imageTaskCurrentHex = "020100000000000000000000000000000000000000A4810000000000008152000000000000FE93056B00000000CBAD350F000000000000000000000000000000000000000000000000000000000000000000000000"

// Pre-mutation counterpart — same mode, size=21099 (delta -22).
const imageTaskPriorHex = "020100000000000000000000000000000000000000A4810000000000006B520000000000006692056B00000000EAC5612F000000000000000000000000000000000000000000000000000000000000000000000000"

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)

	return b
}

func TestDecodeFileInfo_HappyPath_ImageTask(t *testing.T) {
	value := mustHex(t, imageTaskCurrentHex)

	fi, ok := DecodeFileInfo(value)

	require.True(t, ok)
	assert.Equal(t, byte(0x02), fi.Kind, "kind byte is 0x02 for a file Node")
	assert.True(t, fi.Exists, "exists flag populated")
	assert.Equal(t, uint16(0x81A4), fi.Mode, "mode decodes to 0o100644")
	assert.Equal(t, uint64(21121), fi.Size, "size matches the post-mutation byte count")
	assert.Greater(t, fi.ModTimeSec, uint64(1700000000), "mtime seconds decoded as unix timestamp")
	assert.Less(t, fi.ModTimeNsec, uint64(1_000_000_000), "mtime nanoseconds field below 1e9")
}

func TestDecodeFileInfo_SizeDelta(t *testing.T) {
	prior, _ := DecodeFileInfo(mustHex(t, imageTaskPriorHex))
	current, _ := DecodeFileInfo(mustHex(t, imageTaskCurrentHex))

	assert.Equal(t, uint64(21099), prior.Size)
	assert.Equal(t, uint64(21121), current.Size)
	assert.Equal(t, uint64(22), current.Size-prior.Size, "+22 bytes matches the file append")
	assert.Equal(t, prior.Mode, current.Mode, "mode unchanged across the mutation")
	assert.NotEqual(t, prior.ModTimeSec, current.ModTimeSec, "mtime seconds field flipped")
}

func TestDecodeFileInfo_ShortInput(t *testing.T) {
	// 36 bytes — one short of the 37-byte minimum (through size field).
	value := make([]byte, 36)
	value[0] = fileInfoKindExpected

	_, ok := DecodeFileInfo(value)

	assert.False(t, ok, "input below minimum length rejected")
}

func TestDecodeFileInfo_WrongKind(t *testing.T) {
	// 85 bytes long, but first byte isn't 0x02 — caller handed us a
	// Command Value instead of a Node Value.
	value := make([]byte, 85)
	value[0] = 0x01

	_, ok := DecodeFileInfo(value)

	assert.False(t, ok, "wrong kind byte rejected to avoid mis-decode")
}

func TestDecodeFileInfo_OpaqueFieldsEmptyOnAllZeroSample(t *testing.T) {
	// The observed Nuke samples have zero bytes in the head (2..20) and
	// tail (53..end) regions; the decoder suppresses those fields so the
	// JSON output stays compact.
	value := mustHex(t, imageTaskCurrentHex)
	fi, ok := DecodeFileInfo(value)
	require.True(t, ok)

	assert.Empty(t, fi.OpaqueHeadHex, "all-zero head bytes elided")
	assert.Empty(t, fi.OpaqueTailHex, "all-zero tail bytes elided")
}

func TestDecodeFileInfo_OpaqueFieldsPopulatedOnNonZeroTail(t *testing.T) {
	// Flip a tail byte to non-zero and confirm the hex surfaces so a
	// future llbuild schema change doesn't go unnoticed.
	value := mustHex(t, imageTaskCurrentHex)
	value[70] = 0xDE
	value[71] = 0xAD
	fi, ok := DecodeFileInfo(value)
	require.True(t, ok)

	assert.Contains(t, fi.OpaqueTailHex, "dead", "non-zero tail bytes surfaced as hex")
}

func TestFileInfo_Diff_SizeChange(t *testing.T) {
	prior, _ := DecodeFileInfo(mustHex(t, imageTaskPriorHex))
	current, _ := DecodeFileInfo(mustHex(t, imageTaskCurrentHex))

	diff := prior.Diff(current)

	assert.Contains(t, diff, "mode: unchanged (0o100644)")
	assert.Contains(t, diff, "size: 21099 → 21121 bytes (+22)")
	assert.Contains(t, diff, "mtime:")
	assert.Contains(t, diff, "→")
}

func TestFileInfo_Diff_IdenticalValues(t *testing.T) {
	prior, _ := DecodeFileInfo(mustHex(t, imageTaskPriorHex))

	diff := prior.Diff(prior)

	assert.Contains(t, diff, "mode: unchanged")
	assert.Contains(t, diff, "size: unchanged")
	assert.Contains(t, diff, "mtime: unchanged")
	assert.False(t, strings.Contains(diff, "→"), "identical pair should not render arrows")
}

func TestFileInfo_Diff_ModeChange(t *testing.T) {
	prior := FileInfo{Mode: 0o100644, Size: 100}
	current := FileInfo{Mode: 0o100755, Size: 100}

	diff := prior.Diff(current)

	assert.Contains(t, diff, "mode: 0o100644 → 0o100755")
	assert.Contains(t, diff, "size: unchanged")
}
