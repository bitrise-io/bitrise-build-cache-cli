package analyzersidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

// EnvDetailsUpload opts in to uploading the analyzer Report to the analytics
// backend; the local sidecar is written regardless.
const EnvDetailsUpload = "BITRISE_BUILD_CACHE_XCODE_DETAILS_UPLOAD"

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

// DetailsUploadEnabled reports whether EnvDetailsUpload is truthy in envs.
func DetailsUploadEnabled(envs map[string]string) bool {
	enabled, err := strconv.ParseBool(envs[EnvDetailsUpload])

	return err == nil && enabled
}

// NewUploadingWriter returns a sidecar Writer rooted at p that also uploads via
// client when uploadEnabled. client may be nil.
func NewUploadingWriter(p paths.Paths, logger log.Logger, client *analytics.Client, uploadEnabled bool) *UploadingWriter {
	w := &UploadingWriter{Sidecar: NewWriter(p, logger), Logger: logger}

	switch {
	case !uploadEnabled:
		w.debugf("Invocation details upload disabled (%s not set)", EnvDetailsUpload)
	case client == nil:
		w.debugf("Invocation details upload disabled (no analytics client)")
	default:
		w.Client = client
	}

	return w
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
