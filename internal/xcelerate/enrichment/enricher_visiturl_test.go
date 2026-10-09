//go:build unit

package enrichment_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

type recordingAppender struct {
	records []invocations.Record
}

func (r *recordingAppender) Append(rec invocations.Record) error {
	r.records = append(r.records, rec)

	return nil
}

func TestVisitURL_Shape(t *testing.T) {
	assert.Equal(t, "https://app.bitrise.io/build-cache/invocations/xcode/abc", enrichment.VisitURL("abc"))
}

func TestEnricher_AppendsWrapperlessToLocalLog(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}

	appender := &recordingAppender{}

	e := &enrichment.Enricher{
		Store:            store,
		Client:           mock,
		LocalLogAppender: appender,
	}

	entry := enrichment.ManifestEntry{
		UUID:      "wrapperless",
		Signature: "Build MyScheme",
		Status:    "S",
		Start:     time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC),
		Stop:      time.Date(2026, 10, 8, 10, 0, 5, 0, time.UTC),
	}

	e.Enrich("", singleEntryGroup(entry))

	require.Len(t, appender.records, 1, "successful wrapperless PUT must append exactly one local-log record")
	rec := appender.records[0]
	assert.NotEmpty(t, rec.InvocationID)
	assert.Equal(t, invocations.ToolXcode, rec.Tool)
	assert.NotEmpty(t, rec.Command, "command must be derived from manifest Signature")
	assert.Equal(t, 0, rec.ExitCode)
}
