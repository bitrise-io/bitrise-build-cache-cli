package doctor

import (
	"context"

	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func (d *Doctor) ccacheHelperCheck() Check {
	return d.socketCheck(socketCheckParams{
		Name:       "ccache-helper",
		Tool:       toolconfig.Ccache,
		ToolLabel:  "c++",
		SocketPath: d.ccacheSocketPath(),
		Fixer:      StartCcacheHelperFixer(),
		Probe:      ccacheipc.ProbeSocket,
	})
}

// ccacheSocketPath prefers the endpoint activation recorded, for the same reason as
// xcelerateSocketPath: an --ipc-socket-path override never reaches the env chain.
func (d *Doctor) ccacheSocketPath() string {
	if cfg, err := ccacheconfig.ReadConfig(d.osProxy(), utils.DefaultDecoderFactory{}, d.Envs); err == nil && cfg.IPCEndpoint != "" {
		return cfg.IPCEndpoint
	}

	return ccacheconfig.ResolveIPCSocketPath("", d.Envs, d.osProxy())
}

func (d *Doctor) ccacheBinaryCheck() Check {
	return Check{
		Name: "ccache-binary",
		Diagnose: func(_ context.Context) Result {
			if !d.toolActivated(toolconfig.Ccache) {
				return Result{State: StateOK, Detail: "skipped (c++ not activated)"}
			}

			path, err := d.LookPath("ccache")
			if err != nil {
				return Result{State: StateWarn, Detail: "ccache binary not found in PATH. Install via `brew install ccache` if you build C/C++."}
			}

			return Result{State: StateOK, Detail: "found at " + path}
		},
	}
}
