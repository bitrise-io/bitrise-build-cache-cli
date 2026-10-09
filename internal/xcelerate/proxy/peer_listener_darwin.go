//go:build darwin

package proxy

import (
	"net"

	"golang.org/x/sys/unix"
)

// extractPeerPID returns the PID of the process on the other end of a Unix
// socket. Reads LOCAL_PEERPID directly from the live fd via SyscallConn
// (no dup, so the lifetime stays bound to the Conn). Non-UnixConn / lookup
// failure returns (0, err) — the caller treats zero as "unknown PID".
func extractPeerPID(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, nil
	}

	rc, err := uc.SyscallConn()
	if err != nil {
		return 0, err //nolint:wrapcheck // caller treats as "unknown PID"
	}

	var (
		pid    int
		optErr error
	)

	ctlErr := rc.Control(func(fd uintptr) {
		pid, optErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	})
	if ctlErr != nil {
		return 0, ctlErr //nolint:wrapcheck // caller treats as "unknown PID"
	}

	if optErr != nil {
		return 0, optErr //nolint:wrapcheck // caller treats as "unknown PID"
	}

	return pid, nil
}
