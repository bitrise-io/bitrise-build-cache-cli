package enrichment

import (
	"io"

	"github.com/bitrise-io/go-utils/v2/log"
)

//nolint:gochecknoglobals // shared discard logger avoids per-call allocation
var noopLogger = log.NewLogger(log.WithOutput(io.Discard))

func logOr(l log.Logger) log.Logger {
	if l == nil {
		return noopLogger
	}

	return l
}
