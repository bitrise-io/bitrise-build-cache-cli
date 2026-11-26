//go:build unit

package analyzersidecar

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

const uploadTestDay = "2026-11-25"

func uploadTestReport() buildanalyzer.Report {
	return buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{{Key: "Ld /Foo", Target: "Foo", Tool: "linker"}},
	}
}

func fixedDayWriter(p paths.Paths) *Writer {
	return &Writer{Paths: p, Clock: func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) }}
}

// capturingPutter drains each reader it is handed, the way the real client does.
func capturingPutter(err error) (*detailsPutterMock, *[]byte) {
	var sent []byte
	putter := &detailsPutterMock{
		PutInvocationDetailsFunc: func(_ context.Context, _ string, reportJSON io.Reader) error {
			b, readErr := io.ReadAll(reportJSON)
			if readErr != nil {
				return readErr
			}
			sent = b

			return err
		},
	}

	return putter, &sent
}

func TestUploadingWriter_UploadsTheSidecarFileBytes(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	putter, sent := capturingPutter(nil)
	w := &UploadingWriter{Sidecar: fixedDayWriter(p), Client: putter}

	require.NoError(t, w.Write(context.Background(), "inv-1", uploadTestReport()))

	onDisk, err := os.ReadFile(p.AnalyzerSidecarFile(uploadTestDay, "inv-1"))
	require.NoError(t, err)
	assert.Equal(t, string(onDisk), string(*sent))
	require.Len(t, putter.PutInvocationDetailsCalls(), 1)
	assert.Equal(t, "inv-1", putter.PutInvocationDetailsCalls()[0].InvocationID)
}

func TestUploadingWriter_SidecarFailureStreamsTheReport(t *testing.T) {
	sidecarErr := errors.New("disk full")
	sidecar := &sidecarFileWriterMock{
		WriteFileFunc: func(context.Context, string, buildanalyzer.Report) (string, error) { return "", sidecarErr },
	}
	putter, sent := capturingPutter(nil)
	w := &UploadingWriter{Sidecar: sidecar, Client: putter}

	err := w.Write(context.Background(), "inv-1", uploadTestReport())

	require.ErrorIs(t, err, sidecarErr)
	want, marshalErr := json.Marshal(uploadTestReport())
	require.NoError(t, marshalErr)
	assert.JSONEq(t, string(want), string(*sent))
}

func TestUploadingWriter_UnreadableSidecarStreamsTheReport(t *testing.T) {
	sidecar := &sidecarFileWriterMock{
		WriteFileFunc: func(context.Context, string, buildanalyzer.Report) (string, error) {
			return filepath.Join(t.TempDir(), "gone.analyzer.json"), nil
		},
	}
	putter, sent := capturingPutter(nil)
	w := &UploadingWriter{Sidecar: sidecar, Client: putter}

	require.NoError(t, w.Write(context.Background(), "inv-1", uploadTestReport()))

	assert.Contains(t, string(*sent), "Build Foo")
}

func TestUploadingWriter_StreamedEncoderStopsWhenClientReturnsEarly(t *testing.T) {
	putErr := errors.New("refused before reading")
	sidecar := &sidecarFileWriterMock{
		WriteFileFunc: func(context.Context, string, buildanalyzer.Report) (string, error) { return "", nil },
	}
	putter := &detailsPutterMock{
		PutInvocationDetailsFunc: func(context.Context, string, io.Reader) error { return putErr },
	}
	w := &UploadingWriter{Sidecar: sidecar, Client: putter}

	done := make(chan error, 1)
	go func() { done <- w.Write(context.Background(), "inv-1", uploadTestReport()) }()

	select {
	case err := <-done:
		require.ErrorIs(t, err, putErr)
	case <-time.After(5 * time.Second):
		t.Fatal("Write blocked on the encoder goroutine")
	}
}

func TestUploadingWriter_UploadHasItsOwnDeadline(t *testing.T) {
	sidecarPath := filepath.Join(t.TempDir(), "inv-1.analyzer.json")
	require.NoError(t, os.WriteFile(sidecarPath, []byte(`{}`), 0o600))
	sidecar := &sidecarFileWriterMock{
		WriteFileFunc: func(context.Context, string, buildanalyzer.Report) (string, error) { return sidecarPath, nil },
	}

	var (
		uploadErr      error
		uploadDeadline time.Time
	)
	putter := &detailsPutterMock{
		PutInvocationDetailsFunc: func(ctx context.Context, _ string, _ io.Reader) error {
			uploadErr = ctx.Err()
			uploadDeadline, _ = ctx.Deadline()

			return nil
		},
	}
	w := &UploadingWriter{Sidecar: sidecar, Client: putter}

	exhausted, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	start := time.Now()
	require.NoError(t, w.Write(exhausted, "inv-1", uploadTestReport()))

	require.NoError(t, uploadErr, "a parse that used up the caller's deadline must not starve the upload")
	assert.WithinRange(t, uploadDeadline, start.Add(detailsUploadTimeout), time.Now().Add(detailsUploadTimeout))
}

func TestUploadingWriter_UploadErrorStillWritesSidecar(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	uploadErr := errors.New("HTTP 404")
	putter, _ := capturingPutter(uploadErr)
	w := &UploadingWriter{Sidecar: fixedDayWriter(p), Client: putter}

	err := w.Write(context.Background(), "inv-1", uploadTestReport())

	require.ErrorIs(t, err, uploadErr)
	assert.FileExists(t, p.AnalyzerSidecarFile(uploadTestDay, "inv-1"))
}

func TestUploadingWriter_BothErrorsJoined(t *testing.T) {
	sidecarErr := errors.New("disk full")
	uploadErr := errors.New("HTTP 500")
	sidecar := &sidecarFileWriterMock{
		WriteFileFunc: func(context.Context, string, buildanalyzer.Report) (string, error) { return "", sidecarErr },
	}
	putter, _ := capturingPutter(uploadErr)
	w := &UploadingWriter{Sidecar: sidecar, Client: putter}

	err := w.Write(context.Background(), "inv-1", uploadTestReport())

	require.ErrorIs(t, err, sidecarErr)
	require.ErrorIs(t, err, uploadErr)
}

func TestUploadingWriter_NilClientOnlyWritesSidecar(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	w := NewUploadingWriter(p, nil, nil)
	w.Sidecar = fixedDayWriter(p)

	require.NoError(t, w.Write(context.Background(), "inv-1", uploadTestReport()))

	assert.FileExists(t, p.AnalyzerSidecarFile(uploadTestDay, "inv-1"))
}
