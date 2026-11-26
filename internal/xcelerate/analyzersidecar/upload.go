package analyzersidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

const detailsSchemaVersion = 1

type reportWriter interface {
	Write(ctx context.Context, invocationID string, rep buildanalyzer.Report) error
}

type detailsPutter interface {
	PutInvocationDetails(ctx context.Context, invocationID string, body []byte) error
}

type detailsEnvelope struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Report        buildanalyzer.Report `json:"report"`
}

// UploadingWriter writes the local sidecar, then uploads the same Report when
// Client is set. Neither step's failure skips the other.
type UploadingWriter struct {
	Sidecar reportWriter
	Client  detailsPutter
	Logger  log.Logger
}

// NewUploadingWriter returns a sidecar Writer rooted at p that also uploads via
// client; a nil client only writes the sidecar.
func NewUploadingWriter(p paths.Paths, logger log.Logger, client detailsPutter) *UploadingWriter {
	return &UploadingWriter{Sidecar: NewWriter(p, logger), Client: client, Logger: logger}
}

func (w *UploadingWriter) Write(ctx context.Context, invocationID string, rep buildanalyzer.Report) error {
	var errs []error

	if w.Sidecar != nil {
		if err := w.Sidecar.Write(ctx, invocationID, rep); err != nil {
			errs = append(errs, err)
		}
	}

	if w.Client != nil {
		if err := w.upload(ctx, invocationID, rep); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Private — upload

func (w *UploadingWriter) upload(ctx context.Context, invocationID string, rep buildanalyzer.Report) error {
	body, err := json.Marshal(detailsEnvelope{SchemaVersion: detailsSchemaVersion, Report: rep})
	if err != nil {
		return fmt.Errorf("invocation details: encode: %w", err)
	}

	if err := w.Client.PutInvocationDetails(ctx, invocationID, body); err != nil {
		return fmt.Errorf("invocation details: upload: %w", err)
	}

	w.debugf("Invocation details uploaded: %s", invocationID)

	return nil
}

func (w *UploadingWriter) debugf(format string, args ...any) {
	if w.Logger != nil {
		w.Logger.Debugf(format, args...)
	}
}
