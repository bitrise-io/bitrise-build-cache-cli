// Package invocationlog is the single entry point for appending to the local
// NDJSON invocation log (~/.local/state/bitrise-build-cache/invocations/).
package invocationlog

import (
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// Appender is the subset of invocations.Writer used by callers that take an
// interface for test injection.
type Appender interface {
	Append(rec invocations.Record) error
}

// Default returns an Appender backed by the user's $HOME; logger receives
// warn/debug lines. Returns nil + a warn when the home dir cannot be
// resolved, which is the same contract the previous per-caller helpers had.
func Default(logger log.Logger) Appender {
	p, err := paths.Default()
	if err != nil {
		if logger != nil {
			logger.Warnf("Skipping local invocation log: %v", err)
		}

		return nil
	}

	w := invocations.NewWriter(p)
	w.Logger = logger

	return w
}
