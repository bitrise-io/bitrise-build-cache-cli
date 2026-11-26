package buildanalyzer

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// FileInfo mirrors the layout llbuild serializes into a Node rule's Value
// BLOB for a file-stat sample. The layout was reverse-engineered from
// paired snapshots taken before/after a known file mutation (+22 bytes to
// Nuke's ImageTask.swift) and cross-checked against two sibling .swift
// files — mode / size / mtime decode consistently across all three.
//
// Verified layout (byte offsets within the 85-byte on-disk record):
//
//	 0      kind (1 byte — always 0x02 for a file Node)
//	 1      exists (1 byte — nonzero means the file exists)
//	 2..20  opaque reserved (19 bytes — zero on every sample observed;
//	        likely device/inode or an alternative-id field, but we don't
//	        interpret it)
//	21..22  mode (little-endian uint16; 0x81A4 == 0o100644 confirmed)
//	23..28  padding (6 bytes — zero on every sample)
//	29..36  size (little-endian uint64; +22 matched a known file append)
//	37..44  mtime seconds (little-endian uint64 unix timestamp —
//	        decoded to a realistic wall-clock time matching the build run)
//	45..52  mtime nanoseconds (little-endian uint64 — <1e9 on every
//	        sample observed, consistent with a `tv_nsec` field)
//	53..    opaque trailer (32 bytes of zero on observed samples; likely
//	        content hash / inode / reserved for an alternative identity)
//
// See docs/xcode-build-analyzer.md for the uncertainty table.
type FileInfo struct {
	Kind        byte   `json:"kind"`
	Exists      bool   `json:"exists"`
	Mode        uint16 `json:"mode"`
	Size        uint64 `json:"size"`
	ModTimeSec  uint64 `json:"modTimeSec,omitempty"`
	ModTimeNsec uint64 `json:"modTimeNsec,omitempty"`
	// OpaqueHeadHex is bytes 2..20 hex-encoded, preserved for operators
	// inspecting an unusual Value. Zero on every observed sample.
	OpaqueHeadHex string `json:"opaqueHeadHex,omitempty"`
	// OpaqueTailHex is bytes 53..end hex-encoded. Zero on every observed
	// sample but surfaced so a future non-zero case surfaces to the
	// operator without a decoder rewrite.
	OpaqueTailHex string `json:"opaqueTailHex,omitempty"`
}

// fileInfoKindExpected is the byte seen at offset 0 on every Node-rule file
// Value sampled from real build.db snapshots. Command-rule Values do not
// start with this byte, which lets DecodeFileInfo bail safely when handed
// a Command Value by mistake.
const fileInfoKindExpected byte = 0x02

// fileInfoMinLen is the minimum length a Value must have for the
// high-confidence fields (through size) to be readable. The observed
// layout extends through byte 52 — but we accept anything that gets us
// past the size field so a slightly-trimmed record still decodes.
const fileInfoMinLen = 37

// fileInfoFullLen is the full observed-layout length — up through the
// mtime nanoseconds field. Values at or past this length get mtime
// decoded as well; shorter inputs leave mtime at zero.
const fileInfoFullLen = 53

// DecodeFileInfo attempts to parse a Node-rule Value BLOB into a FileInfo.
// Returns (zero, false) when the input is shorter than the minimum layout
// length OR the first byte isn't the expected kind — the latter keeps us
// from mis-decoding a Command Value that happens to be long enough.
//
// Opaque head (bytes 2..20) and tail (bytes 53..end) are preserved as hex
// strings so the operator can still see what's there without the decoder
// inventing fields it hasn't verified.
func DecodeFileInfo(value []byte) (FileInfo, bool) {
	if len(value) < fileInfoMinLen {
		return FileInfo{}, false
	}

	if value[0] != fileInfoKindExpected {
		return FileInfo{}, false
	}

	fi := FileInfo{
		Kind:   value[0],
		Exists: value[1] != 0,
		Mode:   binary.LittleEndian.Uint16(value[21:23]),
		Size:   binary.LittleEndian.Uint64(value[29:37]),
	}

	if head := value[2:21]; !isAllZero(head) {
		fi.OpaqueHeadHex = hex.EncodeToString(head)
	}

	if len(value) >= fileInfoFullLen {
		fi.ModTimeSec = binary.LittleEndian.Uint64(value[37:45])
		fi.ModTimeNsec = binary.LittleEndian.Uint64(value[45:53])

		if tail := value[53:]; !isAllZero(tail) {
			fi.OpaqueTailHex = hex.EncodeToString(tail)
		}
	} else if len(value) > 37 {
		// Partial mtime region — surface the raw bytes without pretending
		// to split them.
		fi.OpaqueTailHex = hex.EncodeToString(value[37:])
	}

	return fi, true
}

// isAllZero reports whether every byte in b is zero. Used to suppress the
// hex-encoded opaque field when it contains no signal, keeping the JSON
// output compact.
func isAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}

	return true
}

// Diff renders a compact one-line description of what changed between a
// prior and current FileInfo. Intended for `--output pretty` where the
// caller indents it under a FileChange header.
//
// Opaque fields are surfaced only when they diverge so the common case
// (all-zero reserved bytes) doesn't pollute the diff line.
func (fi FileInfo) Diff(other FileInfo) string {
	var parts []string

	switch fi.Mode {
	case other.Mode:
		parts = append(parts, fmt.Sprintf("mode: unchanged (0o%o)", fi.Mode))
	default:
		parts = append(parts, fmt.Sprintf("mode: 0o%o → 0o%o", fi.Mode, other.Mode))
	}

	switch fi.Size {
	case other.Size:
		parts = append(parts, fmt.Sprintf("size: unchanged (%d bytes)", fi.Size))
	default:
		delta := int64(other.Size) - int64(fi.Size) //nolint:gosec // size fits in int64 for any realistic file
		parts = append(parts, fmt.Sprintf("size: %d → %d bytes (%+d)", fi.Size, other.Size, delta))
	}

	switch {
	case fi.ModTimeSec == other.ModTimeSec && fi.ModTimeNsec == other.ModTimeNsec:
		if fi.ModTimeSec != 0 || fi.ModTimeNsec != 0 {
			parts = append(parts, "mtime: unchanged")
		}
	default:
		parts = append(parts, fmt.Sprintf("mtime: %d.%09d → %d.%09d (seconds likely)",
			fi.ModTimeSec, fi.ModTimeNsec, other.ModTimeSec, other.ModTimeNsec))
	}

	if fi.OpaqueHeadHex != other.OpaqueHeadHex {
		parts = append(parts, fmt.Sprintf("opaque head: %s → %s",
			hexPrefix(fi.OpaqueHeadHex), hexPrefix(other.OpaqueHeadHex)))
	}

	if fi.OpaqueTailHex != other.OpaqueTailHex {
		parts = append(parts, fmt.Sprintf("opaque tail: %s → %s",
			hexPrefix(fi.OpaqueTailHex), hexPrefix(other.OpaqueTailHex)))
	}

	return strings.Join(parts, "; ")
}

// opaqueHexPrefixLen is the max glyph count for the opaque head/tail hex
// preview surfaced by FileInfo.Diff. Keeps the diff line scannable
// (8 hex chars == 4 bytes, enough to tell two non-zero prefixes apart).
const opaqueHexPrefixLen = 8

// hexPrefix returns up to opaqueHexPrefixLen chars of s, with a trailing
// `…` when truncated, or `<zero>` when s is empty (the "unchanged opaque
// bytes were all zero" case).
func hexPrefix(s string) string {
	if s == "" {
		return "<zero>"
	}

	if len(s) <= opaqueHexPrefixLen {
		return s
	}

	return s[:opaqueHexPrefixLen] + "…"
}
