package ccache

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"
)

func (s *IpcServer) createListener(ctx context.Context) (net.Listener, error) {
	if err := os.Remove(s.config.IPCEndpoint); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove existing socket: %w", err)
	}

	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", s.config.IPCEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on Unix socket: %w", err)
	}

	return listener, nil
}
