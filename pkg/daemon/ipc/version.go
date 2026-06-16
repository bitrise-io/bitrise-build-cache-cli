// Package ipc is the frozen wire contract for the daemon control sockets. See docs/daemon-ipc-protocol.md.
package ipc

// Bumping ProtocolV1 requires a parallel spec-doc bump, fresh golden testdata under testdata/v<N>/, and transition-window kept-compat handling.
const ProtocolV1 = 1

// MaxFrameBytes-exceeding lines MUST cause the receiver to close the connection with a frame_too_large error.
const MaxFrameBytes = 64 * 1024
