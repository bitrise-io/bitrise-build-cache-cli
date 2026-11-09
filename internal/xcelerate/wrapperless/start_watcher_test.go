//go:build unit

package wrapperless

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEventSource struct {
	events chan Event

	mu        sync.Mutex
	watched   []string
	stopOnce  sync.Once
}

func newFakeEventSource() *fakeEventSource {
	return &fakeEventSource{events: make(chan Event, 16)}
}

func (f *fakeEventSource) Events() <-chan Event { return f.events }

func (f *fakeEventSource) Watch(root string, _ ...EventOp) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watched = append(f.watched, root)

	return nil
}

func (f *fakeEventSource) Stop() {
	f.stopOnce.Do(func() { close(f.events) })
}

func writeBuildRequest(t *testing.T, root, hex, action, qos string) string {
	t.Helper()

	dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"),
		[]byte(`{"parameters":{"action":"`+action+`","qos":"`+qos+`"}}`), 0o644))

	return dir
}

func waitForStart(t *testing.T, ch chan Start) Start {
	t.Helper()

	select {
	case s := <-ch:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("no Start emitted")

		return Start{}
	}
}

func TestStartWatcher_FiresOnPrimaryBuildCreate(t *testing.T) {
	fakeHome := t.TempDir()

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")
	hex := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	// Build-request written AFTER Start so the seed walk doesn't pre-fire.
	dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")

	src := newFakeEventSource()
	starts := make(chan Start, 4)

	w := &StartWatcher{
		HomeDir:   fakeHome,
		OnStart:   func(s Start) { starts <- s },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"),
		[]byte(`{"parameters":{"action":"build","qos":""}}`), 0o644))

	src.events <- Event{Path: dir, Op: EventCreate}

	got := waitForStart(t, starts)
	assert.Equal(t, hex, got.BuildRequestHex)
	assert.Equal(t, PrimaryActionBuild, got.Action)
	assert.Equal(t, root, got.DerivedDataPath)

	w.Close()
}

// TestStartWatcher_RejectsSubBuilds verifies the primary-action filter: dark
// build (qos=default) and indexer runs must never emit a Start.
func TestStartWatcher_RejectsSubBuilds(t *testing.T) {
	for _, c := range []struct {
		name, action, qos string
	}{
		{"dark", "build", "default"},
		{"indexer", "indexbuild", "prepareForIndexing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeHome := t.TempDir()

			root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-"+c.name)
			hex := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")

			src := newFakeEventSource()
			starts := make(chan Start, 4)

			w := &StartWatcher{
				HomeDir:   fakeHome,
				OnStart:   func(s Start) { starts <- s },
				NewSource: func() (DirEventSource, error) { return src, nil },
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, w.Start(ctx))

			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"),
				[]byte(`{"parameters":{"action":"`+c.action+`","qos":"`+c.qos+`"}}`), 0o644))

			src.events <- Event{Path: dir, Op: EventCreate}

			select {
			case got := <-starts:
				t.Fatalf("unexpected Start for %s: %+v", c.name, got)
			case <-time.After(50 * time.Millisecond):
			}

			w.Close()
		})
	}
}

func TestStartWatcher_FiresOnArchiveAndTest(t *testing.T) {
	for _, c := range []struct {
		name, action string
		want         PrimaryAction
	}{
		{"archive", "archive", PrimaryActionArchive},
		{"test", "test", PrimaryActionTest},
		{"analyze", "analyze", PrimaryActionAnalyze},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeHome := t.TempDir()
			root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-"+c.name)
			hex := "deadbeef" + c.name + "0000000000000000000000"
			hex = hex[:32]
			dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")

			src := newFakeEventSource()
			starts := make(chan Start, 4)

			w := &StartWatcher{
				HomeDir:   fakeHome,
				OnStart:   func(s Start) { starts <- s },
				NewSource: func() (DirEventSource, error) { return src, nil },
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, w.Start(ctx))

			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"),
				[]byte(`{"parameters":{"action":"`+c.action+`","qos":""}}`), 0o644))

			src.events <- Event{Path: dir, Op: EventCreate}

			got := waitForStart(t, starts)
			assert.Equal(t, c.want, got.Action)

			w.Close()
		})
	}
}

func TestStartWatcher_IgnoresMissingBuildRequest(t *testing.T) {
	fakeHome := t.TempDir()

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")
	hex := "dddddddddddddddddddddddddddddddd"
	dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")

	src := newFakeEventSource()
	starts := make(chan Start, 4)

	w := &StartWatcher{
		HomeDir:   fakeHome,
		OnStart:   func(s Start) { starts <- s },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// Dir exists but build-request.json is missing — must not emit.
	require.NoError(t, os.MkdirAll(dir, 0o755))

	src.events <- Event{Path: dir, Op: EventCreate}

	select {
	case got := <-starts:
		t.Fatalf("unexpected Start with missing build-request.json: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	w.Close()
}

func TestStartWatcher_CloseIdempotent(t *testing.T) {
	w := &StartWatcher{}

	w.Close()
	w.Close()
}

// TestStartWatcher_DeepTreeCreatedAtomically simulates FSEvents delivering a
// single terminal CREATE for an xcbuilddata dir that appeared alongside its
// entire ancestor chain in one mkdir-all. With rjeczalik/notify's recursive
// FSEvents watch there are no intermediate adds to race — the terminal
// CREATE is delivered directly.
func TestStartWatcher_DeepTreeCreatedAtomically(t *testing.T) {
	fakeHome := t.TempDir()

	src := newFakeEventSource()
	starts := make(chan Start, 4)

	w := &StartWatcher{
		HomeDir:   fakeHome,
		OnStart:   func(s Start) { starts <- s },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	ddRoot := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData")
	root := filepath.Join(ddRoot, "MyApp-abcdefghijklmnopqrstuvwxyz0")
	hex := "ffffffffffffffffffffffffffffffff"
	xcbuilddataDir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(xcbuilddataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xcbuilddataDir, "build-request.json"),
		[]byte(`{"parameters":{"action":"build","qos":""}}`), 0o644))

	// FSEvents delivers the terminal CREATE directly. No intermediate
	// events are needed because the watchpoint is recursive.
	src.events <- Event{Path: xcbuilddataDir, Op: EventCreate}

	got := waitForStart(t, starts)
	assert.Equal(t, hex, got.BuildRequestHex)
	assert.Equal(t, PrimaryActionBuild, got.Action)
	assert.Equal(t, root, got.DerivedDataPath)

	w.Close()
}

// TestStartWatcher_PreExistingHexDirsSeedOnStart verifies the seed walk
// catches an xcbuilddata dir that exists before Start is called. Without a
// seed walk the proxy would miss any build already running when it started.
// TestStartWatcher_SeedSkipsStaleCompletedBuild guards the phantom-row path:
// a prior Xcode session's xcbuilddata lingers on disk alongside a sealed
// LogStoreManifest.plist older than startedAt — seed must skip it.
func TestStartWatcher_SeedSkipsStaleCompletedBuild(t *testing.T) {
	fakeHome := t.TempDir()
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-stale-start")
	hex := "abcabcabcabcabcabcabcabcabcabc00"
	_ = writeBuildRequest(t, root, hex, "build", "")

	manifestPath := filepath.Join(root, "Logs", "Build", "LogStoreManifest.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(manifestPath), 0o755))
	require.NoError(t, os.WriteFile(manifestPath, []byte("<plist/>"), 0o644))

	info, err := os.Stat(manifestPath)
	require.NoError(t, err)
	afterManifest := info.ModTime().Add(10 * time.Second)

	src := newFakeEventSource()
	starts := make(chan Start, 4)

	w := &StartWatcher{
		HomeDir:   fakeHome,
		OnStart:   func(s Start) { starts <- s },
		NewSource: func() (DirEventSource, error) { return src, nil },
		Now:       func() time.Time { return afterManifest },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	select {
	case got := <-starts:
		t.Fatalf("Start emitted for stale xcbuilddata: %+v", got)
	case <-time.After(200 * time.Millisecond):
	}

	w.Close()
}

func TestStartWatcher_PreExistingHexDirsSeedOnStart(t *testing.T) {
	fakeHome := t.TempDir()

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-preexisting")
	hex := "77777777777777777777777777777777"
	// Pre-populate BEFORE calling Start.
	_ = writeBuildRequest(t, root, hex, "build", "")

	src := newFakeEventSource()
	starts := make(chan Start, 4)

	w := &StartWatcher{
		HomeDir:   fakeHome,
		OnStart:   func(s Start) { starts <- s },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	got := waitForStart(t, starts)
	assert.Equal(t, hex, got.BuildRequestHex)
	assert.Equal(t, PrimaryActionBuild, got.Action)
	assert.Equal(t, root, got.DerivedDataPath)

	w.Close()
}

// TestStartWatcher_SeedAndStreamDedup verifies the seen-set gates emission
// across both paths: the seed walk fires once, and a later notify CREATE
// for the same dir must be swallowed.
func TestStartWatcher_SeedAndStreamDedup(t *testing.T) {
	fakeHome := t.TempDir()

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-dedup")
	hex := "9999999999999999999999999999aaaa"
	dir := writeBuildRequest(t, root, hex, "build", "")

	src := newFakeEventSource()
	starts := make(chan Start, 4)

	w := &StartWatcher{
		HomeDir:   fakeHome,
		OnStart:   func(s Start) { starts <- s },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// Seed walk fires on Start.
	_ = waitForStart(t, starts)

	// A subsequent CREATE for the same dir must be swallowed by the seen-set.
	src.events <- Event{Path: dir, Op: EventCreate}

	select {
	case got := <-starts:
		t.Fatalf("unexpected duplicate Start: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	w.Close()
}

// TestStartWatcher_EmitsOnBuildRequestJSONWriteAfterDirCreate covers the case
// where the xcbuilddata dir CREATE fires before build-request.json lands. The
// initial emitStartOnce hits ErrNotFound and must not mark the seen-set; the
// later CREATE for build-request.json retries against the parent dir and
// emits exactly one Start.
func TestStartWatcher_EmitsOnBuildRequestJSONWriteAfterDirCreate(t *testing.T) {
	fakeHome := t.TempDir()

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-late")
	hex := "deadbeefdeadbeefdeadbeefdeadbeef"
	dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")

	src := newFakeEventSource()
	starts := make(chan Start, 4)

	w := &StartWatcher{
		HomeDir:   fakeHome,
		OnStart:   func(s Start) { starts <- s },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// Dir exists but build-request.json does not yet — the CREATE for the
	// xcbuilddata dir must not emit.
	require.NoError(t, os.MkdirAll(dir, 0o755))
	src.events <- Event{Path: dir, Op: EventCreate}

	select {
	case got := <-starts:
		t.Fatalf("unexpected Start before build-request.json write: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	// build-request.json lands, followed by a CREATE event for the file
	// path itself. The handler retries against the parent dir and emits.
	buildReqPath := filepath.Join(dir, "build-request.json")
	require.NoError(t, os.WriteFile(buildReqPath,
		[]byte(`{"parameters":{"action":"build","qos":""}}`), 0o644))

	src.events <- Event{Path: buildReqPath, Op: EventCreate}

	got := waitForStart(t, starts)
	assert.Equal(t, hex, got.BuildRequestHex)
	assert.Equal(t, PrimaryActionBuild, got.Action)
	assert.Equal(t, root, got.DerivedDataPath)

	// No duplicate emission from any follow-up event.
	select {
	case got := <-starts:
		t.Fatalf("unexpected duplicate Start: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	w.Close()
}

// TestStartWatcher_InstallsRecursiveWatch asserts the watchpoint is installed
// at the DD root with the .../... recursive suffix — the single piece of
// FSEvents plumbing we rely on instead of the old stair-step add chain.
func TestStartWatcher_InstallsRecursiveWatch(t *testing.T) {
	fakeHome := t.TempDir()

	src := newFakeEventSource()
	w := &StartWatcher{
		HomeDir:   fakeHome,
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))
	defer w.Close()

	ddRoot := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData")
	want := filepath.Join(ddRoot, "...")

	src.mu.Lock()
	defer src.mu.Unlock()
	assert.Contains(t, src.watched, want,
		"expected recursive watch on %s, got %v", want, src.watched)
}
