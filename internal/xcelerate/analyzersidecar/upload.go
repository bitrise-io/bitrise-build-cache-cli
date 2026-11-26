package analyzersidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

// detailsUploadTimeout is detached from the caller's deadline so a slow parse can't starve it; 20s fits the wrapper's 30s exit wait.
const detailsUploadTimeout = 20 * time.Second

//go:generate moq -stub -out sidecar_file_writer_mock_test.go -pkg analyzersidecar . sidecarFileWriter
//go:generate moq -stub -out details_putter_mock_test.go -pkg analyzersidecar . detailsPutter

type sidecarFileWriter interface {
	WriteFile(ctx context.Context, invocationID string, rep buildanalyzer.Report) (string, error)
}

type detailsPutter interface {
	PutInvocationDetails(ctx context.Context, invocationID string, reportJSON io.Reader) error
}

// UploadingWriter writes the local sidecar, then uploads that file when Client
// is set; if the sidecar write failed it streams the Report instead. Neither
// step's failure skips the other.
type UploadingWriter struct {
	Sidecar sidecarFileWriter
	Client  detailsPutter
	Logger  log.Logger
}

// NewUploadingWriter returns a sidecar Writer rooted at p that also uploads via
// client; a nil client only writes the sidecar.
func NewUploadingWriter(p paths.Paths, logger log.Logger, client detailsPutter) *UploadingWriter {
	return &UploadingWriter{Sidecar: NewWriter(p, logger), Client: client, Logger: logger}
}

func (w *UploadingWriter) Write(ctx context.Context, invocationID string, rep buildanalyzer.Report) error {
	var (
		sidecarPath string
		sidecarErr  error
	)
	if w.Sidecar != nil {
		sidecarPath, sidecarErr = w.Sidecar.WriteFile(ctx, invocationID, rep)
	}

	var uploadErr error
	if w.Client != nil {
		uploadErr = w.upload(ctx, invocationID, sidecarPath, rep)
	}

	return errors.Join(sidecarErr, uploadErr)
}

// Private — upload

func (w *UploadingWriter) upload(ctx context.Context, invocationID, sidecarPath string, rep buildanalyzer.Report) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detailsUploadTimeout)
	defer cancel()

	if err := w.put(ctx, invocationID, sidecarPath, rep); err != nil {
		return fmt.Errorf("invocation details: upload: %w", err)
	}

	w.debugf("Invocation details uploaded: %s", invocationID)

	return nil
}

func (w *UploadingWriter) put(ctx context.Context, invocationID, sidecarPath string, rep buildanalyzer.Report) error {
	if sidecarPath != "" {
		f, err := os.Open(sidecarPath)
		if err == nil {
			defer f.Close()

			//nolint:wrapcheck // wrapped by upload
			return w.Client.PutInvocationDetails(ctx, invocationID, f)
		}

		w.debugf("Invocation details: reopen sidecar, streaming report instead: %v", err)
	}

	return w.putStreamed(ctx, invocationID, rep)
}

func (w *UploadingWriter) putStreamed(ctx context.Context, invocationID string, rep buildanalyzer.Report) error {
	pr, pw := io.Pipe()
	encoded := make(chan struct{})

	go func() {
		defer close(encoded)
		_ = pw.CloseWithError(json.NewEncoder(pw).Encode(rep))
	}()

	err := w.Client.PutInvocationDetails(ctx, invocationID, pr)
	// Unblocks the encoder when the client stopped reading early.
	_ = pr.Close()
	<-encoded

	//nolint:wrapcheck // wrapped by upload
	return err
}

func (w *UploadingWriter) debugf(format string, args ...any) {
	if w.Logger != nil {
		w.Logger.Debugf(format, args...)
	}
}
