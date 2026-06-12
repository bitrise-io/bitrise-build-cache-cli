// Package ipc is the **frozen wire contract** for the Bitrise Build Cache
// daemon control sockets (xcelerate proxy + ccache storage helper).
//
// See `docs/daemon-ipc-protocol.md` in the repo root for the authoritative
// spec. The Go types in this package mirror that document one-for-one; the
// golden-file tests in this package keep the encoded form from drifting.
//
// External consumers (the future Native Mac app, custom tooling) import this
// package and use Decoder / Encoder to talk to a running helper without
// having to redo the JSON schema by hand.
package ipc

// ProtocolV1 is the wire-format version currently locked. Servers and
// clients exchange this number in the handshake; mismatches result in a
// protocol_mismatch error.
//
// Bumping this constant requires:
//   - a parallel bump of the spec doc,
//   - regenerated golden testdata under testdata/v<N>/,
//   - kept-compat handling in servers for the previous version during a
//     transition window.
const ProtocolV1 = 1

// MaxFrameBytes caps the size of a single newline-terminated message. Lines
// longer than this MUST cause the receiver to close the connection with a
// frame_too_large error. Picked at 64 KiB to stay well above any plausible
// status / recent_invocations payload while keeping memory bounded on
// hostile peers.
const MaxFrameBytes = 64 * 1024
