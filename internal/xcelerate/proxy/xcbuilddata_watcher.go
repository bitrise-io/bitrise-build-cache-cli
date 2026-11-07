package proxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildrequest"
)

// xcbuilddataSuffix is the directory-name suffix Xcode uses under
// XCBuildData for every per-build-request sub-build. The hex component
// (the directory basename minus the suffix) is the build-request hash the
// enrichment classifier correlates against.
const xcbuilddataSuffix = ".xcbuilddata"

// dirEventSource abstracts fsnotify so the watcher can be exercised
// synchronously in tests without touching the kernel.
type dirEventSource interface {
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Add(path string) error
	Close() error
}

// fsnotifyAdapter wraps *fsnotify.Watcher in dirEventSource.
type fsnotifyAdapter struct {
	w *fsnotify.Watcher
}

func newFsnotifyAdapter() (*fsnotifyAdapter, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("new fsnotify watcher: %w", err)
	}

	return &fsnotifyAdapter{w: w}, nil
}

func (a *fsnotifyAdapter) Events() <-chan fsnotify.Event { return a.w.Events }
func (a *fsnotifyAdapter) Errors() <-chan error          { return a.w.Errors }
func (a *fsnotifyAdapter) Add(path string) error {
	//nolint:wrapcheck // thin adapter; caller wraps if it needs context
	return a.w.Add(path)
}

func (a *fsnotifyAdapter) Close() error {
	//nolint:wrapcheck
	return a.w.Close()
}

// Start boots the .xcbuilddata watcher: seeds the cache from any already-on-
// disk dirs under each DerivedData root, then spawns a goroutine that fsnotifies
// future creates and pre-populates the cache so sidecar-flush lookups never
// touch disk. Strictly additive — any failure degrades to the existing
// on-demand loader with no error returned.
func (c *buildRequestInfoCache) Start(ctx context.Context) error {
	return c.start(ctx, func() (dirEventSource, error) { return newFsnotifyAdapter() })
}

// start factors out the event-source constructor so tests inject a fake.
func (c *buildRequestInfoCache) start(ctx context.Context, newSource func() (dirEventSource, error)) error {
	roots := allDerivedDataRoots()

	c.seed(roots)

	src, err := newSource()
	if err != nil {
		c.warn("xcbuilddata watcher start failed: %s", err)

		return nil
	}

	for _, root := range roots {
		c.addWatchesForRoot(src, root)
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	c.mu.Lock()
	c.watcher = src
	c.watcherCancel = cancel
	c.watcherDone = done
	c.mu.Unlock()

	go c.watchLoop(ctx, src, done)

	return nil
}

// Close stops the watcher goroutine and releases the underlying event source.
// Safe to call when Start was never called (or failed) — both branches are no-op.
func (c *buildRequestInfoCache) Close() {
	c.mu.Lock()
	cancel := c.watcherCancel
	done := c.watcherDone
	src := c.watcher
	c.watcherCancel = nil
	c.watcherDone = nil
	c.watcher = nil
	c.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()

	if src != nil {
		_ = src.Close() //nolint:errcheck // best-effort shutdown
	}

	if done != nil {
		<-done
	}
}

// seed walks every known XCBuildData dir under each DerivedData root and
// pre-populates the cache with every pre-existing .xcbuilddata entry. Covers
// the proxy-restart-mid-build case where fsnotify alone would miss dirs that
// landed before Start.
func (c *buildRequestInfoCache) seed(roots []string) {
	for _, root := range roots {
		for _, dir := range xcbuildDataDirsFor(root) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}

			for _, e := range entries {
				if !e.IsDir() || !strings.HasSuffix(e.Name(), xcbuilddataSuffix) {
					continue
				}

				c.populateFromDir(root, filepath.Join(dir, e.Name()))
			}
		}
	}
}

// addWatchesForRoot installs fsnotify watches on both XCBuildData subtrees
// (Build/ and Index.noindex/Build/) where they exist. When the parent subtree
// is not on disk yet (fresh DD root, Xcode hasn't yet created Index.noindex)
// the watcher also observes the grandparent so a later subtree-create event
// can promote the watch.
func (c *buildRequestInfoCache) addWatchesForRoot(src dirEventSource, root string) {
	for _, dir := range xcbuildDataDirsFor(root) {
		if _, err := os.Stat(dir); err == nil {
			if addErr := src.Add(dir); addErr != nil {
				c.warn("xcbuilddata watch add %s: %s", dir, addErr)
			}

			continue
		}

		// Parent subtree isn't present yet; watch the grandparent so a later
		// Create event under the Build / Index.noindex tree tells us to add
		// the eventual XCBuildData dir on the fly.
		grandparent := filepath.Dir(filepath.Dir(dir))
		if _, err := os.Stat(grandparent); err != nil {
			continue
		}

		if addErr := src.Add(grandparent); addErr != nil {
			c.warn("xcbuilddata watch add %s: %s", grandparent, addErr)
		}
	}
}

// xcbuildDataDirsFor returns the two canonical XCBuildData locations under a
// DerivedData root. Both are probed because Xcode's IDE flow and sub-build
// classifier split dirs across the two subtrees.
func xcbuildDataDirsFor(root string) []string {
	return []string{
		filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData"),
		filepath.Join(root, "Index.noindex", "Build", "Intermediates.noindex", "XCBuildData"),
	}
}

// watchLoop drains the event source, pre-populating the cache for every new
// .xcbuilddata directory. Panics are recovered — a watcher crash must never
// take the proxy down, the on-demand loader still covers correctness.
func (c *buildRequestInfoCache) watchLoop(ctx context.Context, src dirEventSource, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			c.warn("xcbuilddata watcher goroutine panic: %v", r)
		}

		close(done)
	}()

	events := src.Events()
	errs := src.Errors()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}

			c.handleEvent(src, ev)
		case err, ok := <-errs:
			if !ok {
				return
			}

			if err != nil {
				c.warn("xcbuilddata watcher error: %s", err)
			}
		}
	}
}

// handleEvent reacts to a single fsnotify event: a Create for an .xcbuilddata
// dir maps to a cache populate; a Create for any intermediate Build /
// Intermediates.noindex / Index.noindex / XCBuildData parent promotes to an
// XCBuildData watch so the next hex lands correctly. fsnotify is not
// recursive — each intermediate dir must be watched in turn until the real
// XCBuildData dir exists.
func (c *buildRequestInfoCache) handleEvent(src dirEventSource, ev fsnotify.Event) {
	if ev.Op&fsnotify.Create == 0 {
		return
	}

	base := filepath.Base(ev.Name)
	if strings.HasSuffix(base, xcbuilddataSuffix) {
		root := rootForXCBuildDataPath(ev.Name)
		c.populateFromDir(root, ev.Name)

		return
	}

	// Promote: keep adding watches on this newly-created dir if it is on the
	// path to a known XCBuildData subtree. The sub-dir itself is watched so
	// its children (and eventually the XCBuildData dir) surface too.
	switch base {
	case "Build", "Index.noindex", "Intermediates.noindex", "XCBuildData":
		if addErr := src.Add(ev.Name); addErr != nil {
			c.warn("xcbuilddata watch add %s: %s", ev.Name, addErr)
		}
	}
}

// rootForXCBuildDataPath walks a .xcbuilddata path upward to the DerivedData
// workspace root (the dir that contains Build/ or Index.noindex/). Returns ""
// when the shape doesn't match; buildrequest.Load's blank-ddPath fallback
// then handles the empty case.
func rootForXCBuildDataPath(xcbuildDataDir string) string {
	// .../<root>/Build/Intermediates.noindex/XCBuildData/<hex>.xcbuilddata
	// .../<root>/Index.noindex/Build/Intermediates.noindex/XCBuildData/<hex>.xcbuilddata
	dir := filepath.Dir(xcbuildDataDir) // XCBuildData
	dir = filepath.Dir(dir)             // Intermediates.noindex
	dir = filepath.Dir(dir)             // Build
	buildOrIndex := filepath.Base(dir)  // "Build"
	parent := filepath.Dir(dir)         // either root (Build layout) or Index.noindex
	if buildOrIndex == "Build" && filepath.Base(parent) == "Index.noindex" {
		return filepath.Dir(parent)
	}

	return parent
}

// populateFromDir reads the build-request.json inside an .xcbuilddata dir and
// writes the parsed (action, qos) into the shared cache under the blank-ddPath
// key. The same key format feeds the on-demand loader's enumerate-under-$HOME
// fallback, so a watcher populate and a late load for the same hex collapse to
// one entry.
func (c *buildRequestInfoCache) populateFromDir(root, xcbuildDataDir string) {
	base := filepath.Base(xcbuildDataDir)
	hex := strings.TrimSuffix(base, xcbuilddataSuffix)
	if hex == "" {
		return
	}

	info, err := buildrequest.Load(root, hex)
	if err != nil {
		if !errors.Is(err, buildrequest.ErrNotFound) {
			c.warn("xcbuilddata populate %s: %s", xcbuildDataDir, err)
		}

		return
	}

	key := "\x00" + hex

	c.mu.Lock()
	c.cache[key] = info
	c.mu.Unlock()
}

// warn forwards to the configured logger when present; nil-safe for tests.
func (c *buildRequestInfoCache) warn(format string, args ...any) {
	if c.logger == nil {
		return
	}

	c.logger.Warnf(format, args...)
}
