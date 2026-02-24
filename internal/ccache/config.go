package ccache

import "time"

// Config holds the configuration for the ccache IPC server.
type Config struct {
	LogFile     string
	ErrLogFile  string
	IPCEndpoint string
	IdleTimeout time.Duration
	Layout      string
	PushEnabled bool
}
