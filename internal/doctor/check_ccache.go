package doctor

import (
	"context"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func (d *Doctor) ccacheHelperCheck() Check {
	socketPath := ccacheconfig.ResolveIPCSocketPath("", d.Envs, utils.DefaultOsProxy{})

	return d.socketDaemonCheck("ccache-helper", toolconfig.Ccache, "c++", socketPath)
}

func (d *Doctor) ccacheBinaryCheck() Check {
	return Check{
		Name: "ccache-binary",
		Diagnose: func(_ context.Context) Result {
			path, err := d.LookPath("ccache")
			if err != nil {
				return Result{State: StateWarn, Detail: "ccache binary not found in PATH. Install via `brew install ccache` if you build C/C++."}
			}

			return Result{State: StateOK, Detail: "found at " + path}
		},
	}
}
