package buildanalyzer

import (
	"encoding/hex"
	"fmt"
)

// valueBytesPreviewLen caps the hex-preview of a Value BLOB. 128 bytes is
// well past the fixed FileInfo prefix (40 bytes) and leaves room for both
// opaque trailing slots plus future growth, while keeping the JSON row
// scannable. When the BLOB is longer the hex string gets an ASCII "…"
// marker appended so the operator sees the truncation.
const valueBytesPreviewLen = 128

// buildValueDiff returns the four byte-level diff fields (prior hex / current
// hex / first differing byte offset / human note) for a pair of Value
// BLOBs. Safe to call on empty inputs — an empty pair yields zero-value
// returns the callers can leave the JSON fields unset for.
//
// When one side is empty and the other isn't, the populated side is still
// surfaced via its hex preview but the offset is reported as 0 (first byte
// is where the difference starts, by definition). When both are populated
// and share a common prefix but differ in length, the offset is reported
// as the length of the shorter slice — the first byte past the prefix is
// where the "difference" surfaces.
func buildValueDiff(prior, current []byte) (string, string, int, string) {
	if len(prior) == 0 && len(current) == 0 {
		return "", "", 0, ""
	}

	priorHex := hexPreview(prior, valueBytesPreviewLen)
	currentHex := hexPreview(current, valueBytesPreviewLen)
	offset := firstDifferingByte(prior, current)
	note := describeValueDiff(prior, current, offset)

	return priorHex, currentHex, offset, note
}

// firstDifferingByte returns the index of the first byte at which a and b
// differ, or -1 when the common prefix is equal AND the lengths differ, or
// 0 when the slices are identical (callers filter identical pairs before
// calling buildValueDiff; -1 keeps the "sizes differ" signal distinct).
func firstDifferingByte(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}

	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}

	if len(a) != len(b) {
		return -1
	}

	return 0
}

// describeValueDiff builds the one-line human note. Prefers the byte-offset
// form `"byte N: 0xXXXX → 0xYYYY (delta +D, size changed)"` when a concrete
// byte flipped, falls back to a size-only note when only the length
// differs, and an empty note when nothing could be compared.
func describeValueDiff(prior, current []byte, offset int) string {
	sizeChanged := len(prior) != len(current)

	if offset == -1 {
		return fmt.Sprintf("sizes differ (%d → %d bytes); common prefix matches entirely", len(prior), len(current))
	}

	if offset == 0 && len(prior) > 0 && len(current) > 0 && prior[0] == current[0] {
		if sizeChanged {
			return fmt.Sprintf("sizes differ (%d → %d bytes); identical", len(prior), len(current))
		}

		return ""
	}

	priorBytes := snippetBytes(prior, offset)
	currentBytes := snippetBytes(current, offset)

	parts := fmt.Sprintf("byte %d: 0x%s → 0x%s", offset, priorBytes, currentBytes)

	if sizeChanged {
		return fmt.Sprintf("%s (size: %d → %d bytes)", parts, len(prior), len(current))
	}

	return parts
}

// snippetBytes returns up to 2 hex-encoded bytes starting at offset. Keeps
// the human note compact — `0x6B52 → 0x8152` reads cleaner than dumping
// the full 8-byte little-endian slot.
func snippetBytes(b []byte, offset int) string {
	if offset < 0 || offset >= len(b) {
		return ""
	}

	end := offset + 2
	if end > len(b) {
		end = len(b)
	}

	return hex.EncodeToString(b[offset:end])
}

// hexPreview returns the hex encoding of the first n bytes of b, with an
// ASCII `…` sentinel appended when truncated. Empty input yields empty
// output so the caller's omitempty tag hides unused rows.
func hexPreview(b []byte, n int) string {
	if len(b) == 0 {
		return ""
	}

	if len(b) <= n {
		return hex.EncodeToString(b)
	}

	return hex.EncodeToString(b[:n]) + "…"
}
