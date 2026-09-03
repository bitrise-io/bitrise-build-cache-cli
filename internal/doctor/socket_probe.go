package doctor

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

type socketCheckParams struct {
	Name       string
	Tool       toolconfig.Tool
	ToolLabel  string
	SocketPath string
	Fixer      Fixer

	Handshake spawn.Handshake
}

func (d *Doctor) socketCheck(p socketCheckParams) Check {
	return Check{
		Name: p.Name,
		Diagnose: func(ctx context.Context) Result {
			if !d.toolActivated(p.Tool) {
				return Result{State: StateOK, Detail: "skipped (" + p.ToolLabel + " not activated)"}
			}

			switch spawn.Probe(ctx, p.SocketPath, p.Handshake) {
			case spawn.Stopped:
				return Result{State: StateWarn, Detail: "not running (no socket file)", Fixable: true, Fixer: p.Fixer}
			case spawn.Stuck:
				return Result{
					State:   StateWarn,
					Detail:  "stuck: socket " + p.SocketPath + " present but not answering — fixable",
					Fixable: true,
					Fixer:   p.Fixer,
				}
			case spawn.Running:
			}

			return Result{State: StateOK, Detail: "running (" + p.SocketPath + ")"}
		},
	}
}
