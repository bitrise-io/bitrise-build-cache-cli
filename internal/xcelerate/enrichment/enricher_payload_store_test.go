//go:build unit

package enrichment_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestEnricher_PayloadStoreUpsertedOnSuccessfulPUT(t *testing.T) {
	dir := t.TempDir()
	pstore := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}

	e := &enrichment.Enricher{Client: mock, PayloadStore: pstore}

	manifestPath, logName := stageOKLog(t)
	entry := enrichment.ManifestEntry{
		UUID:      "manifest-xyz",
		Signature: "Build MyScheme",
		FileName:  logName,
		Start:     time.Now(),
		Stop:      time.Now().Add(2 * time.Second),
	}
	e.Enrich(manifestPath, singleEntryGroup(entry))

	records, err := pstore.Load()
	require.NoError(t, err)
	require.Len(t, records, 1, "exactly one payload record must land for the PUT")
	assert.Equal(t, "manifest-xyz", records[0].InvocationID)
	assert.Equal(t, enrichment.PayloadStatusEmitted, records[0].Status)
	assert.Equal(t, "ok", records[0].Merged.LogOutcome)
}
