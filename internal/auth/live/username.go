package live

import (
	"os/user"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// UsernameSource is where a display name came from.
type UsernameSource int

const (
	UsernameSourceNone UsernameSource = iota
	UsernameSourceEnv
	UsernameSourceStored
	UsernameSourceOS
)

func (s UsernameSource) String() string {
	switch s {
	case UsernameSourceEnv:
		return "env"
	case UsernameSourceStored:
		return "credential store"
	case UsernameSourceOS:
		return "OS username fallback"
	case UsernameSourceNone:
		return "none"
	}

	return "unknown"
}

// ResolveUsername names the person behind a local invocation, for analytics
// attribution. Deliberately separate from Resolve: `auth username` writes the name
// independently of the token, so finding it costs a store read that the per-RPC
// callers must not pay for a value they never use.
func (r *Resolver) ResolveUsername(envs map[string]string) (string, UsernameSource) {
	if v := strings.TrimSpace(envs[auth.EnvUsername]); v != "" {
		return v, UsernameSourceEnv
	}

	for _, s := range r.backends() {
		if ts, err := s.Load(); err == nil {
			if v := strings.TrimSpace(ts.Username); v != "" {
				return v, UsernameSourceStored
			}
		}
	}

	if u, err := user.Current(); err == nil {
		if v := strings.TrimSpace(u.Username); v != "" {
			return v, UsernameSourceOS
		}
	}

	return "", UsernameSourceNone
}
