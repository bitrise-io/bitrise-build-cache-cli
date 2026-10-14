// Package buildidentity derives a deterministic InvocationID from a peer
// process anchor (hostname + pid + start-ms). Shared by the wrapper-less path:
// trampoline/proxy mint the same ID without any out-of-band handshake.
package buildidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// Anchor is the (hostname, pid, start-ms) triple that identifies a long-lived
// xcodebuild/Xcode/SWBBuildService process.
type Anchor struct {
	Hostname    string
	PID         int
	StartTimeMS int64
}

// AncestryEntry is one step of a peer process's ancestry: (pid, exe basename,
// start-ms). Produced by proxy/ancestry and injected here so this package
// stays import-light.
type AncestryEntry struct {
	PID         int
	Name        string
	StartTimeMS int64
}

// anchorableNames is the set of process names a peer ancestry must contain
// before we derive an InvocationID. Keeps health probes and unrelated
// socket consumers out of the sidecar emission path.
//
//nolint:gochecknoglobals // immutable table
var anchorableNames = map[string]struct{}{
	"xcodebuild":      {},
	"Xcode":           {},
	"SWBBuildService": {},
}

// Derive returns a UUIDv4-shaped InvocationID from the anchor. sha256 is
// hashed over hostname||pid||start-ms; the first 16 bytes are formatted into
// 8-4-4-4-12 hex with the version/variant nibbles stamped so it round-trips
// through parsers that insist on v4. v4+variant nibbles cost 6 bits of input, leaving 122 effective bits of entropy.
func Derive(a Anchor) string {
	h := sha256.New()
	h.Write([]byte(a.Hostname))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.Itoa(a.PID)))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.FormatInt(a.StartTimeMS, 10)))

	sum := h.Sum(nil)
	buf := sum[:16]
	buf[6] = (buf[6] & 0x0f) | 0x40 // v4
	buf[8] = (buf[8] & 0x3f) | 0x80 // RFC 4122 variant

	hexBuf := hex.EncodeToString(buf)

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hexBuf[0:8], hexBuf[8:12], hexBuf[12:16], hexBuf[16:20], hexBuf[20:32])
}

// AnchorFromAncestry walks the peer's ancestry (deepest-first, i.e. starting
// at the peer itself) and returns the first entry whose exe name is in
// anchorableNames. Returns (_, false) when no anchorable ancestor is found —
// callers must NOT fall through to the peer PID in that case; the resulting
// InvocationID would overcount health probes and unrelated connections.
func AnchorFromAncestry(hostname string, ancestry []AncestryEntry) (Anchor, bool) {
	for _, e := range ancestry {
		if _, ok := anchorableNames[e.Name]; ok {
			return Anchor{Hostname: hostname, PID: e.PID, StartTimeMS: e.StartTimeMS}, true
		}
	}

	return Anchor{}, false
}
