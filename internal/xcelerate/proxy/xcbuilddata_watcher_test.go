//go:build unit

package proxy

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

// fakeEventSource is a dirEventSource backed by a caller-driven chan.
// Lets tests inject .xcbuilddata Create events synchronously.
type fakeEventSource struct {
	events chan fsnotify.Event
	errs   chan error

	mu       sync.Mutex
	added    []string
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

func (f *fakeEventSource) addedSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.added))
	copy(out, f.added)

	return out
}

func (f *fakeEventSource) Close() error {
	f.closeOnce.Do(func() {
		close(f.events)
		close(f.errs)
	})

	return nil
}

// writeBuildRequest materialises the <hex>.xcbuilddata layout under a fake
// DerivedData root, writing a parseable build-request.json. Returns the dir
// path the caller then feeds to the fake event source.
func writeBuildRequest(t *testing.T, root, hex, action, qos string) string {
	t.Helper()

	dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"),
		[]byte(`{"parameters":{"action":"`+action+`","qos":"`+qos+`"}}`), 0o644))

	return dir
}

// waitFor polls fn until it returns true or the deadline hits.
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("condition not met within deadline")
}

func TestXcbuildDataWatcher_PopulatesOnCreate(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc123")

	hex := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dir := writeBuildRequest(t, root, hex, "build", "default")

	c := newBuildRequestInfoCache(nil)
	src := newFakeEventSource()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, c.start(ctx, func() (dirEventSource, error) { return src, nil }))

	src.events <- fsnotify.Event{Name: dir, Op: fsnotify.Create}

	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, ok := c.cache["\x00"+hex]

		return ok
	})

	c.mu.Lock()
	info := c.cache["\x00"+hex]
	c.mu.Unlock()

	assert.Equal(t, "build", info.Action)
	assert.Equal(t, "default", info.QoS)

	c.Close()
}

func TestXcbuildDataWatcher_SeedsPreExistingDirs(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc123")

	hex := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	writeBuildRequest(t, root, hex, "indexbuild", "prepareForIndexing")

	c := newBuildRequestInfoCache(nil)
	src := newFakeEventSource()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, c.start(ctx, func() (dirEventSource, error) { return src, nil }))

	c.mu.Lock()
	info, ok := c.cache["\x00"+hex]
	c.mu.Unlock()

	require.True(t, ok, "seed must prepopulate pre-existing .xcbuilddata dirs")
	assert.Equal(t, "indexbuild", info.Action)
	assert.Equal(t, "prepareForIndexing", info.QoS)

	c.Close()
}

func TestXcbuildDataWatcher_IgnoresNonXCBuildDataEvents(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc123")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData"), 0o755))

	c := newBuildRequestInfoCache(nil)
	src := newFakeEventSource()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, c.start(ctx, func() (dirEventSource, error) { return src, nil }))

	// A Create for an unrelated path must not add any entries.
	src.events <- fsnotify.Event{Name: filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", "something-else"), Op: fsnotify.Create}

	// A Write event for an .xcbuilddata path must also be ignored (only Create fires the populate).
	src.events <- fsnotify.Event{Name: filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", "cccccccccccccccccccccccccccccccc.xcbuilddata"), Op: fsnotify.Write}

	// Give the loop a moment to drain.
	time.Sleep(20 * time.Millisecond)

	c.mu.Lock()
	n := len(c.cache)
	c.mu.Unlock()

	assert.Equal(t, 0, n, "unrelated events must not populate the cache")

	c.Close()
}

func TestXcbuildDataWatcher_MissingBuildRequestJSONNoCache(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc123")
	hex := "dddddddddddddddddddddddddddddddd"
	// Create the .xcbuilddata dir but do NOT write build-request.json.
	dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	c := newBuildRequestInfoCache(nil)
	src := newFakeEventSource()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, c.start(ctx, func() (dirEventSource, error) { return src, nil }))

	src.events <- fsnotify.Event{Name: dir, Op: fsnotify.Create}

	time.Sleep(20 * time.Millisecond)

	c.mu.Lock()
	_, ok := c.cache["\x00"+hex]
	c.mu.Unlock()

	assert.False(t, ok, "missing build-request.json must not populate the cache")

	c.Close()
}

func TestXcbuildDataWatcher_NewSourceErrorFallsBack(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	hex := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc123")
	writeBuildRequest(t, root, hex, "build", "default")

	c := newBuildRequestInfoCache(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Even when the event source fails to init, Start must return nil and the
	// seed pass must still have populated the cache. The on-demand loader
	// remains fully functional.
	require.NoError(t, c.start(ctx, func() (dirEventSource, error) {
		return nil, assert.AnError
	}))

	c.mu.Lock()
	_, ok := c.cache["\x00"+hex]
	c.mu.Unlock()

	assert.True(t, ok, "seed must still populate the cache when fsnotify init fails")

	// Close must be safe with no watcher attached.
	c.Close()
}

func TestXcbuildDataWatcher_CloseIdempotent(t *testing.T) {
	c := newBuildRequestInfoCache(nil)

	// Never called Start — Close must still be a no-op.
	c.Close()
	c.Close()
}

func TestXcbuildDataWatcher_PromotesParentCreateToXCBuildDataWatch(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc123")
	// Create only the DD root — no Build/Intermediates subtree yet. Start
	// should watch the DD root as the grandparent.
	require.NoError(t, os.MkdirAll(root, 0o755))

	c := newBuildRequestInfoCache(nil)
	src := newFakeEventSource()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, c.start(ctx, func() (dirEventSource, error) { return src, nil }))

	// Simulate Xcode rolling out the XCBuildData subtree one dir at a time.
	// Each intermediate Create event must add a watch on the newly-created
	// dir so the eventual XCBuildData create lands inside a watched path.
	buildDir := filepath.Join(root, "Build")
	intermediates := filepath.Join(buildDir, "Intermediates.noindex")
	xcbuildData := filepath.Join(intermediates, "XCBuildData")

	src.events <- fsnotify.Event{Name: buildDir, Op: fsnotify.Create}
	src.events <- fsnotify.Event{Name: intermediates, Op: fsnotify.Create}
	src.events <- fsnotify.Event{Name: xcbuildData, Op: fsnotify.Create}

	waitFor(t, func() bool {
		want := map[string]bool{buildDir: false, intermediates: false, xcbuildData: false}
		for _, p := range src.addedSnapshot() {
			if _, ok := want[p]; ok {
				want[p] = true
			}
		}

		for _, got := range want {
			if !got {
				return false
			}
		}

		return true
	})

	c.Close()
}
