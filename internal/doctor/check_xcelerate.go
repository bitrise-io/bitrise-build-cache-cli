package doctor

import (
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func (d *Doctor) xcelerateProxyCheck() Check {
	socketPath := xceleratconfig.ResolveProxySocketPath("", d.Envs, utils.DefaultOsProxy{})

	return d.socketDaemonCheck("xcelerate-proxy", toolconfig.Xcelerate, "xcode", socketPath)
}
