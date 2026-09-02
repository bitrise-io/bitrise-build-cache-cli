package doctor

import (
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func (d *Doctor) xcelerateProxyCheck() Check {
	// Started on demand by the xcodebuild wrapper, not supervised, so the
	// remedy is to spawn one rather than to poke a service manager.
	return d.socketDaemonCheck(
		"xcelerate-proxy", toolconfig.Xcelerate, "xcode", d.xcelerateSocketPath(),
		StartProxyFixer{}, StartProxyFixer{},
	)
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
