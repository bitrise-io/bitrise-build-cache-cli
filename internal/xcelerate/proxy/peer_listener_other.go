//go:build !darwin

package proxy

import "net"

// extractPeerPID is a no-op on non-darwin builds. The proxy only ships on
// macOS today; keeping the stub lets tests compile on linux CI.
func extractPeerPID(_ net.Conn) (int, error) {
	return 0, nil
}
