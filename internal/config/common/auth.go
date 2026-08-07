package common

import (
	"os/user"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

type UsernameSource int

const (
	UsernameSourceNone UsernameSource = iota
	UsernameSourceEnv
	UsernameSourceResolved
	UsernameSourceOS
)

func (s UsernameSource) String() string {
	switch s {
	case UsernameSourceEnv:
		return "env"
	case UsernameSourceResolved:
		return "credential"
	case UsernameSourceOS:
		return "os"
	case UsernameSourceNone:
		return "none"
	}

	return "unknown"
}

// ResolveUsername names the person behind a local invocation. The credential
// already carries a display name when one was set; the OS user is the fallback.
func ResolveUsername(envs map[string]string, cred auth.Credential) (string, UsernameSource) {
	return resolveUsername(envs, cred, osUsername)
}

func resolveUsername(envs map[string]string, cred auth.Credential, osResolver func() string) (string, UsernameSource) {
	if v := strings.TrimSpace(envs[auth.EnvUsername]); v != "" {
		return v, UsernameSourceEnv
	}
	if v := strings.TrimSpace(cred.Username); v != "" {
		return v, UsernameSourceResolved
	}
	if v := strings.TrimSpace(osResolver()); v != "" {
		return v, UsernameSourceOS
	}

	return "", UsernameSourceNone
}

func osUsername() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}

	return u.Username
}
