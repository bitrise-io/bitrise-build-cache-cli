//go:build unit

package xcode

import (
	"context"
	"os"
	"testing"
	"time"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
)

//nolint:gochecknoglobals // reused across all bundle-facing internal tests
var bundleTestLogger = newBundleTestLogger()

func newBundleTestLogger() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	// go-utils Logger interface has many methods; make them all no-ops.
	for _, name := range []string{"Debugf", "Infof", "Warnf", "Errorf", "Printf",
		"TDebugf", "TInfof", "TWarnf", "TErrorf", "TDonef", "TPrintf", "Donef", "Println"} {
		l.On(name, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	}
	l.On("EnableDebugLog", mock.Anything).Return()

	return l
}

func noopCommandFunc(_ string, _ ...string) (string, error) { return "", nil }

func newBundleForTest(t *testing.T, home string, xcodebuildPath string) *analyticsBundle {
	t.Helper()

	// Point HOME at a temp dir so paths.Default() succeeds under test.
	t.Setenv("HOME", home)

	cfg := xcelerate.Config{
		AuthConfig: common.CacheAuthConfig{
			AuthToken:   "test-token",
			WorkspaceID: "test-ws",
		},
		OriginalXcodebuildPath: xcodebuildPath,
	}

	ap := common.NewCachingAuthResolver(time.Hour, nil, bundleTestLogger)

	return newAnalyticsBundle(context.Background(), cfg, map[string]string{}, noopCommandFunc, bundleTestLogger, ap)
}

func Test_newAnalyticsBundle_populatesFieldsHappyPath(t *testing.T) {
	home := t.TempDir()
	// xcodebuildPath="" → xcodeversion.Resolve errors → version stays empty; that's
	// the "empty path" case; here we still exercise the happy path for client/pending.
	b := newBundleForTest(t, home, "")

	require.NotNil(t, b)
	assert.NotNil(t, b.client, "analytics client should be constructed")
	require.NotNil(t, b.pending, "pending store should be constructed when paths.Default succeeds")

	expected := paths.FromHome(home)
	assert.Equal(t, expected.PendingInvocationsFile(), b.pending.Path)
	assert.Equal(t, expected.EnrichmentHealthFile(), b.healthPath)
	assert.Equal(t, home, b.homeDir)
}

func Test_newAnalyticsBundle_emptyXcodePath_leavesVersionEmpty(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")

	assert.Empty(t, b.xcodeVersion)
	assert.Empty(t, b.xcodeBuildNumber)
}

func Test_newAnalyticsBundle_xcodeResolveError_leavesVersionEmptyWithoutPanic(t *testing.T) {
	home := t.TempDir()
	// Non-empty path but a commandFunc that always errors.
	cfg := xcelerate.Config{
		AuthConfig:             common.CacheAuthConfig{AuthToken: "t", WorkspaceID: "ws"},
		OriginalXcodebuildPath: "/does/not/matter/xcodebuild",
	}
	t.Setenv("HOME", home)
	errCmd := func(_ string, _ ...string) (string, error) {
		return "", assert.AnError
	}

	ap := common.NewCachingAuthResolver(time.Hour, nil, bundleTestLogger)
	b := newAnalyticsBundle(context.Background(), cfg, map[string]string{}, errCmd, bundleTestLogger, ap)

	require.NotNil(t, b)
	assert.Empty(t, b.xcodeVersion)
	assert.Empty(t, b.xcodeBuildNumber)
	// paths + client still populated.
	require.NotNil(t, b.pending)
	require.NotNil(t, b.client)
}

func Test_analyticsBundle_enrichmentEnabled(t *testing.T) {
	// Build a fully-populated bundle then knock out one field at a time.
	full := newBundleForTest(t, t.TempDir(), "")
	require.True(t, full.enrichmentEnabled(), "sanity: default bundle should be enabled")

	t.Run("all fields populated → true", func(t *testing.T) {
		b := *full

		assert.True(t, b.enrichmentEnabled())
	})

	t.Run("client nil → false", func(t *testing.T) {
		b := *full
		b.client = nil

		assert.False(t, b.enrichmentEnabled())
	})

	t.Run("pending nil → false", func(t *testing.T) {
		b := *full
		b.pending = nil

		assert.False(t, b.enrichmentEnabled())
	})

	t.Run("homeDir empty → false", func(t *testing.T) {
		b := *full
		b.homeDir = ""

		assert.False(t, b.enrichmentEnabled())
	})
}

func Test_analyticsBundle_watcher_populatedFields(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")

	w := b.watcher(bundleTestLogger)

	require.NotNil(t, w)
	assert.Equal(t, home, w.HomeDir)
	assert.NotNil(t, w.Handle, "Watcher.Handle must be wired to Enricher.Enrich")
	assert.Same(t, bundleTestLogger, w.Logger)
	assert.NotNil(t, w.MatchProbe)
	assert.Equal(t, enrichment.DefaultMaxCorrelationRetries, w.MaxCorrelationRetries)
}

func Test_analyticsBundle_watcher_globsIncludeManagedDD(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")

	w := b.watcher(bundleTestLogger)

	require.NotNil(t, w)
	assert.Contains(t, w.Globs, enrichment.DefaultDerivedDataGlob,
		"default Library/Developer DerivedData glob must be observed")
	assert.Contains(t, w.Globs, paths.XcodeManagedDerivedDataManifestGlobRelative,
		"wrapper-managed .bitrise/cache/xcode-dd glob must be observed")
}

func Test_analyticsBundle_watcher_matchProbe_returnsTrueOnOverlap(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")

	start := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	require.NoError(t, b.pending.Append(enrichment.PendingRecord{
		InvocationID: "inv-1",
		StartTime:    start,
		Duration:     60_000, // ms — 1 minute window
		HitRate:      0.5,
	}))

	w := b.watcher(bundleTestLogger)

	// Entry that fully overlaps the pending record's window.
	entry := enrichment.ManifestEntry{
		UUID:  "u1",
		Start: start.Add(5 * time.Second),
		Stop:  start.Add(30 * time.Second),
	}
	assert.True(t, w.MatchProbe(entry))
}

func Test_analyticsBundle_watcher_matchProbe_returnsFalseOnNoMatch(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")

	start := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	require.NoError(t, b.pending.Append(enrichment.PendingRecord{
		InvocationID: "inv-1",
		StartTime:    start,
		Duration:     60_000,
	}))

	w := b.watcher(bundleTestLogger)

	// Entry far in the future — no overlap.
	entry := enrichment.ManifestEntry{
		UUID:  "u1",
		Start: start.Add(1 * time.Hour),
		Stop:  start.Add(1 * time.Hour).Add(1 * time.Minute),
	}
	assert.False(t, w.MatchProbe(entry))
}

func Test_analyticsBundle_watcher_matchProbe_returnsFalseWhenPendingNil(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")
	b.pending = nil

	w := b.watcher(bundleTestLogger)

	entry := enrichment.ManifestEntry{
		UUID:  "u1",
		Start: time.Now(),
		Stop:  time.Now().Add(time.Minute),
	}
	assert.False(t, w.MatchProbe(entry))
}

func Test_analyticsBundle_retrier_populatedFields(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")

	r := b.retrier(bundleTestLogger)

	require.NotNil(t, r)
	require.NotNil(t, r.Store)
	assert.Equal(t, b.pending, r.Store, "Retrier.Store must share the bundle's pending store")
	require.NotNil(t, r.Client)
	assert.Same(t, bundleTestLogger, r.Logger)
	// Interval and MaxAge intentionally left zero: Retrier.Run applies defaults.
	assert.Equal(t, time.Duration(0), r.Interval)
	assert.Equal(t, time.Duration(0), r.MaxAge)
}

func Test_slimInvocationEmitter_EmitSlim_persistsPendingRecord(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")
	e := &slimInvocationEmitter{bundle: b}

	invID := "test-inv-123"
	start := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	stop := start.Add(2500 * time.Millisecond)

	e.EmitSlim(context.Background(), proxy.SessionMeta{
		InvocationID: invID,
		StartTime:    start,
		EndTime:      stop,
	}, proxy.SessionStats{Hits: 3, Misses: 1})

	records, err := b.pending.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)

	rec := records[0]
	assert.Equal(t, invID, rec.InvocationID)
	assert.True(t, rec.StartTime.Equal(start), "start time preserved")
	assert.Equal(t, int64(2500), rec.Duration, "duration is stop-start in ms")
	assert.InDelta(t, 0.75, rec.HitRate, 1e-6)
}

func Test_slimInvocationEmitter_EmitSlim_noPendingStore_doesNotPanic(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")
	b.pending = nil
	e := &slimInvocationEmitter{bundle: b}

	// Async PUT will run; the test only checks the sync path doesn't nil-deref.
	assert.NotPanics(t, func() {
		e.EmitSlim(context.Background(), proxy.SessionMeta{
			InvocationID: "no-pending",
			StartTime:    time.Now(),
			EndTime:      time.Now().Add(time.Second),
		}, proxy.SessionStats{})
	})
	// Confirm the pending file was not created — pending==nil skips the append.
	_, err := os.Stat(paths.FromHome(home).PendingInvocationsFile())
	assert.True(t, os.IsNotExist(err), "pending file must not exist when b.pending is nil")
}

func Test_resolveInactivityTimeout(t *testing.T) {
	t.Run("unset returns zero", func(t *testing.T) {
		got := resolveInactivityTimeout(map[string]string{}, bundleTestLogger)

		assert.Equal(t, time.Duration(0), got)
	})

	t.Run("empty returns zero", func(t *testing.T) {
		got := resolveInactivityTimeout(map[string]string{xcelerate.EnvInactivityTimeout: ""}, bundleTestLogger)

		assert.Equal(t, time.Duration(0), got)
	})

	t.Run("valid seconds parsed", func(t *testing.T) {
		got := resolveInactivityTimeout(map[string]string{xcelerate.EnvInactivityTimeout: "5s"}, bundleTestLogger)

		assert.Equal(t, 5*time.Second, got)
	})

	t.Run("valid milliseconds parsed", func(t *testing.T) {
		got := resolveInactivityTimeout(map[string]string{xcelerate.EnvInactivityTimeout: "500ms"}, bundleTestLogger)

		assert.Equal(t, 500*time.Millisecond, got)
	})

	t.Run("garbage returns zero and warns", func(t *testing.T) {
		l := newBundleTestLogger()
		got := resolveInactivityTimeout(map[string]string{xcelerate.EnvInactivityTimeout: "not-a-duration"}, l)

		assert.Equal(t, time.Duration(0), got)
		l.AssertCalled(t, "Warnf", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})
}

func Test_slimInvocationEmitter_EmitSlim_skipsWhenMarkerPresent(t *testing.T) {
	home := t.TempDir()
	b := newBundleForTest(t, home, "")
	// bundleForTest already t.Setenv("HOME", home); paths.Default() now resolves to it.
	e := &slimInvocationEmitter{bundle: b}

	invID := "handled-inv"
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateHandledInvocationDir(), 0o755))
	marker := p.XcelerateHandledInvocationFile(invID)
	require.NoError(t, os.WriteFile(marker, nil, 0o644))

	e.EmitSlim(context.Background(), proxy.SessionMeta{
		InvocationID: invID,
		StartTime:    time.Now(),
		EndTime:      time.Now().Add(time.Second),
	}, proxy.SessionStats{Hits: 1})

	// Pending is now appended even when the marker exists so F2 can Correlate the wrapper build.
	records, err := b.pending.Load()
	require.NoError(t, err)
	require.Len(t, records, 1, "pending record must be appended so F2 can correlate the wrapper build back to this InvocationID")
	assert.Equal(t, invID, records[0].InvocationID)

	// Marker survives — startup PruneAll reclaims it after HandledMarkerMaxAge.
	_, err = os.Stat(marker)
	require.NoError(t, err, "marker must survive F1 observation so F2 can honour it")
}

func Test_sweepStaleHandledMarkers_removesOldOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateHandledInvocationDir(), 0o755))

	stale := p.XcelerateHandledInvocationFile("stale")
	fresh := p.XcelerateHandledInvocationFile("fresh")
	require.NoError(t, os.WriteFile(stale, nil, 0o644))
	require.NoError(t, os.WriteFile(fresh, nil, 0o644))

	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	enrichment.PruneAll(paths.FromHome(home), time.Now(), bundleTestLogger)

	_, err := os.Stat(stale)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(fresh)
	assert.NoError(t, err)
}
