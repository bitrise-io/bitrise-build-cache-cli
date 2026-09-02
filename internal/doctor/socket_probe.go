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

	// Handshake proves the service answers; nil means a plain dial suffices.
	Handshake spawn.Handshake
}

// socketCheck reports a service as running iff its socket answers.
func (d *Doctor) socketCheck(p socketCheckParams) Check {
	return Check{
		Name: p.Name,
		Diagnose: func(ctx context.Context) Result {
			if !d.toolActivated(p.Tool) {
				return Result{State: StateOK, Detail: "skipped (" + p.ToolLabel + " not activated)"}
			}

			state := spawn.Probe(ctx, p.SocketPath)
			if p.Handshake != nil {
				state = spawn.ProbeWith(ctx, p.SocketPath, p.Handshake)
			}

			switch state {
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
				return Result{State: StateOK, Detail: "running (" + p.SocketPath + ")"}
			}

			return Result{State: StateOK, Detail: "running (" + p.SocketPath + ")"}
		},
	}
}
