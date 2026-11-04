// Package trustedurl vets endpoint URLs that come from the build environment before
// a credential is sent to them.
package trustedurl

import (
	"net/url"
	"slices"
	"strings"
)

//nolint:gochecknoglobals
var (
	trustedDomains = []string{"bitrise.io", "bitrise.dev"}
	// allowLoopback is only ever set by AllowLoopbackForTests, which exists in test builds.
	allowLoopback bool
)

// Allowed reports whether raw uses one of the schemes and names a host that is a
// Bitrise domain or a subdomain of one. Userinfo is rejected. The empty scheme
// admits a bare host[:port].
func Allowed(raw string, schemes ...string) bool {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	if allowLoopback && isLoopback(host) {
		return true
	}
	if !slices.Contains(schemes, parsed.Scheme) {
		return false
	}
	for _, domain := range trustedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}

	return false
}

// FromEnv returns the trimmed value of key when it is an allowed URL, else "".
func FromEnv(envs map[string]string, key string, schemes ...string) string {
	value := strings.TrimSpace(envs[key])
	if !Allowed(value, schemes...) {
		return ""
	}

	return value
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
