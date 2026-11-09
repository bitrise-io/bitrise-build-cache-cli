package proxy

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless"
)

// WrapperlessPipeline is the proxy's view of the opt-in single-ID pipeline:
// only RegisterPeer is exercised from here (on TagConn). Full lifecycle lives
// in cmd/xcode where the pipeline is constructed.
type WrapperlessPipeline interface {
	RegisterPeer(pid int, ancestry wrapperless.Ancestry, startTimeMS int64)
}

// SetWrapperlessPipeline wires the opt-in pipeline into the sidecar stats
// handler. Idempotent; a nil pipeline disables the hook. No-op when sidecar
// writes are not enabled (sidecarStats is nil until SidecarOptions.Dir is set).
func (p *Proxy) SetWrapperlessPipeline(pipeline WrapperlessPipeline) {
	if p == nil || p.sidecarStats == nil {
		return
	}

	p.sidecarStats.wrapperlessPipeline = pipeline
}

// classifyAncestryKind walks the peer's ancestry (deepest-first) and reports
// whether it belongs to an IDE (Xcode or SWBBuildService ancestor), a CLI
// (xcodebuild ancestor), or neither. Returns the anchor PID + start-ms along
// with the kind so the caller can Register the anchorable ancestor directly —
// the per-conn peer PID (often SwiftCompile or clang) lives for a single RPC
// and would make the QuitWatcher fire spuriously on every finished compile.
func classifyAncestryKind(entries []AncestryEntry) (wrapperless.Ancestry, int, int64) {
	for _, e := range entries {
		switch e.Name {
		case "Xcode", "SWBBuildService":
			return wrapperless.AncestryIDE, e.PID, e.StartTimeMS
		case "xcodebuild":
			return wrapperless.AncestryCLI, e.PID, e.StartTimeMS
		}
	}

	return wrapperless.AncestryUnknown, 0, 0
}
