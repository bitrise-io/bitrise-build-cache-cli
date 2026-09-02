package doctor

import (
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func (d *Doctor) xcelerateProxyCheck() Check {
	// No handshake: a gRPC health check costs more than it is worth here, so a
	// proxy that accepts but never answers reads as running. See the gap noted
	// in docs/daemon-latency.md.
	return d.socketCheck(socketCheckParams{
		Name:       "xcelerate-proxy",
		Tool:       toolconfig.Xcelerate,
		ToolLabel:  "xcode",
		SocketPath: d.xcelerateSocketPath(),
		Fixer:      StartProxyFixer(),
	})
}

// xcelerateSocketPath prefers the path activation recorded, because that is the one
// the proxy listens on and the compiler is handed. It can come from a
// --proxy-socket-path override that the env-or-default chain knows nothing about,
// so re-resolving would probe a socket nobody is listening on.
func (d *Doctor) xcelerateSocketPath() string {
	if cfg, err := xceleratconfig.ReadConfig(d.osProxy(), utils.DefaultDecoderFactory{}, d.Envs); err == nil && cfg.ProxySocketPath != "" {
		return cfg.ProxySocketPath
	}

	return xceleratconfig.ResolveProxySocketPath("", d.Envs, d.osProxy())
}
