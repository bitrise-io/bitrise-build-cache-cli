package ccache

import (
	"time"
)

type Config struct {
	LogFile     string        `json:"logFile,omitempty"`
	ErrLogFile  string        `json:"errLogFile,omitempty"`
	IPCEndpoint string        `json:"ipcEndpoint,omitempty"`
	IdleTimeout time.Duration `json:"idleTimeout,omitempty"`
	Layout      string        `json:"layout,omitempty"`
}
