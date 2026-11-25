//go:build unit

package enrichment

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

// analyzerSidecarTestPutter captures PutInvocation calls without failing them
// so Enrich() always reaches the post-appendLocalLog spawn site.
type analyzerSidecarTestPutter struct {
	mu    sync.Mutex
	calls []analytics.Invocation
}

func (p *analyzerSidecarTestPutter) PutInvocation(inv analytics.Invocation) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls = append(p.calls, inv)

	return nil
}

// stageOKLogDirect is a package-internal helper mirroring stageOKLog in
// enricher_test.go. Duplicated because enricher_test.go lives in
// enrichment_test and is not importable from package enrichment.
func stageOKLogDirect(t *testing.T) (manifestPath, logName string) {
	t.Helper()

	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	logName = "AAAA-BBBB.xcactivitylog"

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write([]byte("note: 7 hits / 10 cacheable tasks (70%)\n"))
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, logName), buf.Bytes(), 0o600))

	return filepath.Join(manifestDir, "LogStoreManifest.plist"), logName
}

func analyzerSidecarEntry(logName string) ManifestEntry {
	now := time.Now()

	return ManifestEntry{ //nolint:exhaustruct
		UUID:      "manifest-anchor-uuid",
		Signature: "Build MyScheme",
		FileName:  logName,
		Status:    "S",
		Start:     now,
		Stop:      now.Add(3 * time.Second),
	}
}

func TestEnricher_ProcessGroup_FiresAnalyzerSidecarExactlyOnce(t *testing.T) {
	sidecar := &analyzerSidecarWriterMock{
		WriteFunc: func(_ context.Context, _ string, _ buildanalyzer.Report) error { return nil },
	}

	spawnCalls := 0
	e := &Enricher{ //nolint:exhaustruct
		Client:          &analyzerSidecarTestPutter{},
		AnalyzerSidecar: sidecar,
	}
	e.spawnFn = func(fn func()) {
		spawnCalls++
		fn()
	}

	manifestPath, logName := stageOKLogDirect(t)
	e.Enrich(manifestPath, ManifestEntryGroup{Entries: []ManifestEntry{analyzerSidecarEntry(logName)}})

	assert.Equal(t, 1, spawnCalls, "successful enriched PUT with sibling xcactivitylog must spawn exactly once")
}

func TestEnricher_ProcessGroup_SkipsSidecarWhenLogPathEmpty(t *testing.T) {
	sidecar := &analyzerSidecarWriterMock{
		WriteFunc: func(_ context.Context, _ string, _ buildanalyzer.Report) error { return nil },
	}

	spawnCalls := 0
	e := &Enricher{ //nolint:exhaustruct
		Client:          &analyzerSidecarTestPutter{},
		AnalyzerSidecar: sidecar,
	}
	e.spawnFn = func(fn func()) {
		spawnCalls++
		fn()
	}

	// No FileName → activityLogPath returns "". Drive Enrich via an override
	// group so the no-xcactivitylog outcome gate (hit-rate OK) doesn't short-
	// circuit the PUT before the sidecar spawn site.
	now := time.Now()
	group := SyntheticManifestGroup("override-src", now, now.Add(time.Second), "Build MyScheme", "MyScheme", "override-invocation-id")

	e.Enrich("", group)

	assert.Zero(t, spawnCalls, "no activity log path → no spawn")
	assert.Empty(t, sidecar.WriteCalls())
}

func TestEnricher_ProcessGroup_SkipsSidecarWhenNil(t *testing.T) {
	spawnCalls := 0
	e := &Enricher{ //nolint:exhaustruct
		Client: &analyzerSidecarTestPutter{},
	}
	e.spawnFn = func(fn func()) {
		spawnCalls++
		fn()
	}

	manifestPath, logName := stageOKLogDirect(t)
	e.Enrich(manifestPath, ManifestEntryGroup{Entries: []ManifestEntry{analyzerSidecarEntry(logName)}})

	assert.Zero(t, spawnCalls, "nil AnalyzerSidecar → no spawn")
}
