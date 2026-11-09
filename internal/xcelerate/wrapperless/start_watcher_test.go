//go:build unit

package wrapperless

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEventSource struct {
	events chan fsnotify.Event
	errs   chan error

	mu        sync.Mutex
	added     []string
	closeOnce sync.Once
}

func newFakeEventSource() *fakeEventSource {
	return &fakeEventSource{
		events: make(chan fsnotify.Event, 16),
		errs:   make(chan error, 4),
	}
}

func (f *fakeEventSource) Events() <-chan fsnotify.Event { return f.events }
func (f *fakeEventSource) Errors() <-chan error          { return f.errs }
func (f *fakeEventSource) Add(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, path)

	return nil
}

func (f *fakeEventSource) Close() error {
	f.closeOnce.Do(func() {
		close(f.events)
		close(f.errs)
	})

	return nil
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

	src.events <- fsnotify.Event{Name: dir, Op: fsnotify.Create}

	got := waitForStart(t, starts)
	assert.Equal(t, hex, got.BuildRequestHex)
	assert.Equal(t, PrimaryActionBuild, got.Action)
	assert.Equal(t, root, got.DerivedDataPath)

	w.Close()
}

func TestStartWatcher_IgnoresDarkBuildQoSDefault(t *testing.T) {
	fakeHome := t.TempDir()

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")
	hex := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	dir := writeBuildRequest(t, root, hex, "build", "default")

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

	src.events <- fsnotify.Event{Name: dir, Op: fsnotify.Create}

	select {
	case got := <-starts:
		t.Fatalf("unexpected Start for dark build: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	w.Close()
}

func TestStartWatcher_IgnoresIndexBuild(t *testing.T) {
	fakeHome := t.TempDir()

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")
	hex := "cccccccccccccccccccccccccccccccc"
	dir := writeBuildRequest(t, root, hex, "indexbuild", "prepareForIndexing")

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

	src.events <- fsnotify.Event{Name: dir, Op: fsnotify.Create}

	select {
	case got := <-starts:
		t.Fatalf("unexpected Start for indexer: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	w.Close()
}

func TestStartWatcher_FiresOnArchiveAndTest(t *testing.T) {
	fakeHome := t.TempDir()

	for _, c := range []struct {
		name, action string
		want         PrimaryAction
	}{
		{"archive", "archive", PrimaryActionArchive},
		{"test", "test", PrimaryActionTest},
		{"analyze", "analyze", PrimaryActionAnalyze},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-"+c.name)
			hex := "deadbeef" + c.name + "0000000000000000000000"
			hex = hex[:32]
			dir := writeBuildRequest(t, root, hex, c.action, "")

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

			src.events <- fsnotify.Event{Name: dir, Op: fsnotify.Create}

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
	require.NoError(t, os.MkdirAll(dir, 0o755))

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

	src.events <- fsnotify.Event{Name: dir, Op: fsnotify.Create}

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
