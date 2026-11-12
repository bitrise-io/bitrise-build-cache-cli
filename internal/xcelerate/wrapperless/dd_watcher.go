package wrapperless

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"howett.net/plist"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildrequest"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/manifestsig"
)

const (
	xcbuilddataSuffix = ".xcbuilddata"
	buildDirName      = "Build"
	eventChanBuffer   = 64
	seedStaleGrace    = 5 * time.Second
)

type EventOp uint32

const (
	EventCreate EventOp = 1 << iota
	EventWrite
)

type Event struct {
	Path string
	Op   EventOp
}

type DirEventSource interface {
	Events() <-chan Event
	Watch(root string, ops ...EventOp) error
	Stop()
}

// DDWatcher is the single DerivedData watcher that merges the former
// StartWatcher and EndWatcher: one source, one goroutine, one seed walk, one
// handled-UUID ledger. Dispatches events by basename to the Start and End
// consumers registered via OnStart / OnEnd.
type DDWatcher struct {
	HomeDir   string
	Logger    log.Logger
	OnStart   func(Start)
	OnEnd     func(End)
	SeenStore *HandledManifestStore
	NewSource func() (DirEventSource, error)
	Now       func() time.Time

	mu        sync.Mutex
	src       DirEventSource
	cancel    context.CancelFunc
	done      chan struct{}
	startedAt time.Time

	seenMu       sync.Mutex
	seenHexDirs  map[string]struct{}
	seenManifest map[string]struct{}
	handled      map[string]struct{}
}

func (w *DDWatcher) Start(ctx context.Context) error {
	w.seenMu.Lock()
	if w.seenHexDirs == nil {
		w.seenHexDirs = make(map[string]struct{})
	}
	if w.seenManifest == nil {
		w.seenManifest = make(map[string]struct{})
	}
	w.seenMu.Unlock()

	w.hydrateHandled()

	newSource := w.NewSource
	if newSource == nil {
		logger := w.Logger
		newSource = func() (DirEventSource, error) { return newRecwatchAdapter(logger), nil }
	}

	src, err := newSource()
	if err != nil {
		w.warn("DDWatcher source init: %s", err)

		return nil
	}

	w.infof("DDWatcher: source init ok")

	ddRoots := derivedDataRoots(w.HomeDir)
	w.infof("DDWatcher: dd roots = %v", ddRoots)

	for _, root := range ddRoots {
		w.installWatch(src, root)
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	w.mu.Lock()
	w.src = src
	w.cancel = cancel
	w.done = done
	w.startedAt = w.now()
	w.mu.Unlock()

	// Single-pass seed walk: xcbuilddata CREATE emissions for Start consumers,
	// LogStoreManifest.plist scans for End consumers, handled-UUID marking for
	// stale-mtime / handled-sibling-manifest skips.
	xcbuildSeeded, manifestsFound := 0, 0
	for _, root := range ddRoots {
		xc, mf := w.seedWalk(root)
		xcbuildSeeded += xc
		manifestsFound += mf
	}
	w.infof("DDWatcher: seed walk done (xcbuilddata=%d manifests=%d)", xcbuildSeeded, manifestsFound)

	go w.loop(ctx, src, done)
	w.infof("DDWatcher: loop goroutine started")

	return nil
}

func (w *DDWatcher) Close() {
	w.mu.Lock()
	cancel := w.cancel
	src := w.src
	done := w.done
	w.cancel = nil
	w.src = nil
	w.done = nil
	w.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()

	if src != nil {
		src.Stop()
	}

	if done != nil {
		<-done
	}
}

func (w *DDWatcher) installWatch(src DirEventSource, ddRoot string) {
	if mkErr := os.MkdirAll(ddRoot, 0o755); mkErr != nil {
		w.warn("DDWatcher mkdir %s: %s", ddRoot, mkErr)

		return
	}

	if addErr := src.Watch(recursivePath(ddRoot), EventCreate, EventWrite); addErr != nil {
		w.warn("DDWatcher watch %s: %s", ddRoot, addErr)

		return
	}

	w.infof("DDWatcher: watching %s recursively", recursivePath(ddRoot))
}

func (w *DDWatcher) loop(ctx context.Context, src DirEventSource, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.warn("DDWatcher goroutine panic: %v", r)
		}

		close(done)
	}()

	w.infof("DDWatcher: loop entered")

	events := src.Events()

	for {
		select {
		case <-ctx.Done():
			w.infof("DDWatcher: ctx done, exiting")

			return
		case ev, ok := <-events:
			if !ok {
				w.infof("DDWatcher: event chan closed, exiting")

				return
			}
			w.handleEvent(ev)
		}
	}
}

func (w *DDWatcher) handleEvent(ev Event) {
	w.debugf("DDWatcher: handleEvent %s", ev.Path)

	base := filepath.Base(ev.Path)

	switch {
	case strings.HasSuffix(base, xcbuilddataSuffix):
		if ev.Op&EventCreate == 0 {
			return
		}
		w.handleXCBuildData(ev.Path)
	case base == "build-request.json":
		if ev.Op&(EventCreate|EventWrite) == 0 {
			return
		}
		w.handleXCBuildData(filepath.Dir(ev.Path))
	case base == "LogStoreManifest.plist":
		if ev.Op&(EventWrite|EventCreate) == 0 {
			return
		}
		w.scanManifest(ev.Path)
	}
}

func (w *DDWatcher) seedWalk(ddRoot string) (int, int) {
	xcbuildCount := 0
	manifestCount := 0

	rootManifest := filepath.Join(ddRoot, "Logs", buildDirName, "LogStoreManifest.plist")
	if info, statErr := os.Stat(rootManifest); statErr == nil {
		if w.scanOrMarkSeen(rootManifest, info) {
			manifestCount++
		}
	}

	entries, err := os.ReadDir(ddRoot)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(ddRoot, e.Name(), "Logs", buildDirName, "LogStoreManifest.plist")
			info, statErr := os.Stat(path)
			if statErr != nil {
				continue
			}
			if w.scanOrMarkSeen(path, info) {
				manifestCount++
			}
		}
	}

	_ = filepath.WalkDir(ddRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir //nolint:nilerr // skip unreadable subtree, keep walking siblings
		}
		if !d.IsDir() {
			return nil
		}

		if strings.HasSuffix(d.Name(), xcbuilddataSuffix) {
			if w.isStaleCompletedBuild(path) {
				w.debugf("DDWatcher: skip seed %s reason=stale-sibling-manifest", path)

				return filepath.SkipDir
			}
			if w.isHandledByManifest(path) {
				w.debugf("DDWatcher: skip seed %s reason=handled-sibling-manifest", path)

				return filepath.SkipDir
			}
			xcbuildCount++
			w.emitStartOnce(path)

			return filepath.SkipDir
		}

		return nil
	})

	return xcbuildCount, manifestCount
}

func (w *DDWatcher) scanOrMarkSeen(path string, info os.FileInfo) bool {
	if w.isStaleManifest(info) {
		w.debugf("DDWatcher: stale-seed %s mtime=%s started=%s", path, info.ModTime().Format(time.RFC3339Nano), w.startedAt.Format(time.RFC3339Nano))
		w.markManifestUUIDsSeen(path)

		return false
	}

	w.scanManifest(path)

	return true
}

func (w *DDWatcher) markManifestUUIDsSeen(path string) {
	entries, err := loadManifestEntries(path)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.UniqueIdentifier == "" || !isSealedTerminal(e.HighLevelStatus) {
			continue
		}

		w.seenMu.Lock()
		_, already := w.seenManifest[e.UniqueIdentifier]
		if !already {
			w.seenManifest[e.UniqueIdentifier] = struct{}{}
		}
		w.seenMu.Unlock()

		if !already {
			w.persistSeen(e.UniqueIdentifier)
		}
	}
}

func (w *DDWatcher) scanManifest(path string) {
	entries, err := loadManifestEntries(path)
	if err != nil {
		w.warn("DDWatcher load %s: %s", path, err)

		return
	}

	for _, e := range entries {
		if !isSealedTerminal(e.HighLevelStatus) {
			continue
		}
		if e.TimeStopped == 0 || e.UniqueIdentifier == "" {
			continue
		}

		w.seenMu.Lock()
		if _, ok := w.seenManifest[e.UniqueIdentifier]; ok {
			w.seenMu.Unlock()

			continue
		}
		w.seenManifest[e.UniqueIdentifier] = struct{}{}
		w.seenMu.Unlock()

		w.persistSeen(e.UniqueIdentifier)

		if stopped := cfAbsToTime(e.TimeStopped); !stopped.IsZero() && !w.startedAt.IsZero() &&
			stopped.Before(w.startedAt.Add(-seedStaleGrace)) {
			w.debugf("DDWatcher: skip pre-start uuid=%s stopped=%s started=%s",
				e.UniqueIdentifier, stopped.Format(time.RFC3339Nano), w.startedAt.Format(time.RFC3339Nano))

			continue
		}

		action := actionFromSignature(e.Signature)
		if action == "" {
			w.debugf("DDWatcher: skip uuid=%s reason=non-primary signature=%q", e.UniqueIdentifier, e.Signature)

			continue
		}

		if w.OnEnd == nil {
			continue
		}

		scheme := e.SchemeName
		success := e.HighLevelStatus != "E" && e.HighLevelStatus != "F"
		logName := e.FileName
		if logName == "" {
			logName = e.UniqueIdentifier + ".xcactivitylog"
		}
		logPath := filepath.Join(filepath.Dir(path), logName)
		w.infof("DDWatcher: emit End uuid=%s scheme=%s status=%s", e.UniqueIdentifier, scheme, e.HighLevelStatus)
		w.OnEnd(End{
			At:                w.now(),
			Scheme:            scheme,
			Project:           manifestsig.FirstSubmatch(manifestsig.Project, e.Signature),
			Workspace:         manifestsig.FirstSubmatch(manifestsig.Workspace, e.Signature),
			Configuration:     manifestsig.FirstSubmatch(manifestsig.Configuration, e.Signature),
			Action:            action,
			Success:           success,
			UniqueIdentifier:  e.UniqueIdentifier,
			Duration:          cfAbsDuration(e.TimeStarted, e.TimeStopped),
			XCActivityLogPath: logPath,
		})
	}
}

func (w *DDWatcher) handleXCBuildData(xcbuildDataDir string) {
	w.emitStartOnce(xcbuildDataDir)
}

func (w *DDWatcher) emitStartOnce(xcbuildDataDir string) {
	hex := strings.TrimSuffix(filepath.Base(xcbuildDataDir), xcbuilddataSuffix)
	if hex == "" {
		return
	}

	root := rootForXCBuildDataPath(xcbuildDataDir)

	info, err := buildrequest.Load(root, hex)
	if err != nil {
		if !errors.Is(err, buildrequest.ErrNotFound) {
			w.warn("DDWatcher load build-request %s: %s", xcbuildDataDir, err)
		}

		return
	}

	action, ok := classifyPrimary(info)
	if !ok {
		w.debugf("DDWatcher: reject hex=%s action=%s qos=%s (not primary)", hex, info.Action, info.QoS)

		return
	}

	if !w.markHexSeen(xcbuildDataDir) {
		return
	}

	if w.OnStart == nil {
		return
	}

	w.infof("DDWatcher: emit Start hex=%s action=%s qos=%s", hex, info.Action, info.QoS)

	w.OnStart(Start{
		At:              w.now(),
		DerivedDataPath: root,
		BuildRequestHex: hex,
		Action:          action,
	})
}

func (w *DDWatcher) markHexSeen(xcbuildDataDir string) bool {
	w.seenMu.Lock()
	defer w.seenMu.Unlock()

	if _, ok := w.seenHexDirs[xcbuildDataDir]; ok {
		return false
	}

	w.seenHexDirs[xcbuildDataDir] = struct{}{}

	return true
}

func (w *DDWatcher) isStaleManifest(info os.FileInfo) bool {
	if w.startedAt.IsZero() {
		return false
	}

	return info.ModTime().Before(w.startedAt.Add(-seedStaleGrace))
}

func (w *DDWatcher) isStaleCompletedBuild(xcbuildDataDir string) bool {
	if w.startedAt.IsZero() {
		return false
	}

	root := rootForXCBuildDataPath(xcbuildDataDir)
	manifest := filepath.Join(root, "Logs", buildDirName, "LogStoreManifest.plist")

	info, err := os.Stat(manifest)
	if err != nil {
		return false
	}

	return info.ModTime().Before(w.startedAt.Add(-seedStaleGrace))
}

func (w *DDWatcher) isHandledByManifest(xcbuildDataDir string) bool {
	if len(w.handled) == 0 {
		return false
	}

	root := rootForXCBuildDataPath(xcbuildDataDir)
	manifest := filepath.Join(root, "Logs", buildDirName, "LogStoreManifest.plist")

	entries, err := loadManifestEntries(manifest)
	if err != nil || len(entries) == 0 {
		return false
	}

	hasHandled := false
	for _, e := range entries {
		if !isSealedTerminal(e.HighLevelStatus) {
			return false
		}
		if _, ok := w.handled[e.UniqueIdentifier]; ok {
			hasHandled = true
		}
	}

	return hasHandled
}

func (w *DDWatcher) hydrateHandled() {
	if w.SeenStore == nil {
		return
	}

	uuids, err := w.SeenStore.LoadFresh(w.now(), HandledManifestMaxAge)
	if err != nil {
		w.warn("DDWatcher hydrate seen store: %s", err)

		return
	}

	if err := w.SeenStore.Compact(w.now(), HandledManifestMaxAge); err != nil {
		w.warn("DDWatcher compact seen store: %s", err)
	}

	set := make(map[string]struct{}, len(uuids))
	for _, u := range uuids {
		set[u] = struct{}{}
	}

	w.mu.Lock()
	w.handled = set
	w.mu.Unlock()

	w.seenMu.Lock()
	if w.seenManifest == nil {
		w.seenManifest = make(map[string]struct{}, len(uuids))
	}
	for u := range set {
		w.seenManifest[u] = struct{}{}
	}
	w.seenMu.Unlock()

	w.infof("DDWatcher: hydrated %d uuids from seen store", len(uuids))
}

func (w *DDWatcher) persistSeen(uuid string) {
	if w.SeenStore == nil {
		return
	}

	if err := w.SeenStore.Append(HandledManifest{UUID: uuid, HandledAt: w.now()}); err != nil {
		w.warn("DDWatcher persist seen uuid=%s: %s", uuid, err)
	}
}

func (w *DDWatcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}

	return time.Now()
}

func (w *DDWatcher) warn(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Warnf(format, args...)
}

func (w *DDWatcher) infof(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Infof(format, args...)
}

func (w *DDWatcher) debugf(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Debugf(format, args...)
}

// classifyPrimary returns the PrimaryAction when the build-request represents
// a user-facing build (qos absent), archive, test, or analyze.
func classifyPrimary(info buildrequest.Info) (PrimaryAction, bool) {
	switch info.Action {
	case "build":
		if info.QoS == "" {
			return PrimaryActionBuild, true
		}
	case "archive":
		return PrimaryActionArchive, true
	case "test":
		return PrimaryActionTest, true
	case "analyze":
		return PrimaryActionAnalyze, true
	}

	return "", false
}

func rootForXCBuildDataPath(xcbuildDataDir string) string {
	dir := filepath.Dir(xcbuildDataDir)
	dir = filepath.Dir(dir)
	dir = filepath.Dir(dir)
	buildOrIndex := filepath.Base(dir)
	parent := filepath.Dir(dir)
	if buildOrIndex == buildDirName && filepath.Base(parent) == "Index.noindex" {
		return filepath.Dir(parent)
	}

	return parent
}

func recursivePath(dir string) string {
	return filepath.Join(dir, "...")
}

// isSealedTerminal reports whether the highLevelStatus marks a completed log
// entry Xcode will not append to again. S=succeeded, W=warning, F=failed, E=error.
func isSealedTerminal(status string) bool {
	return status == "S" || status == "W" || status == "F" || status == "E"
}

//nolint:gochecknoglobals // immutable table
var signatureVerbToAction = map[string]PrimaryAction{
	"Build":     PrimaryActionBuild,
	"Building":  PrimaryActionBuild,
	"Archive":   PrimaryActionArchive,
	"Archiving": PrimaryActionArchive,
	"Test":      PrimaryActionTest,
	"Testing":   PrimaryActionTest,
	"Analyze":   PrimaryActionAnalyze,
	"Analyzing": PrimaryActionAnalyze,
}

func actionFromSignature(sig string) PrimaryAction {
	s := strings.TrimSpace(sig)
	if s == "" {
		return ""
	}

	verb, _, _ := strings.Cut(s, " ")

	return signatureVerbToAction[verb]
}

func cfAbsDuration(start, stop float64) time.Duration {
	if start == 0 || stop == 0 || stop <= start {
		return 0
	}

	return time.Duration((stop - start) * float64(time.Second))
}

//nolint:gochecknoglobals // package-level constant; cheap to compute once.
var cfEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

func cfAbsToTime(sec float64) time.Time {
	if sec == 0 {
		return time.Time{}
	}

	return cfEpoch.Add(time.Duration(sec * float64(time.Second)))
}

type manifestLogEntry struct {
	UniqueIdentifier  string                     `plist:"uniqueIdentifier"`
	Signature         string                     `plist:"signature"`
	SchemeName        string                     `plist:"schemeIdentifier-schemeName"`
	HighLevelStatus   string                     `plist:"highLevelStatus"`
	FileName          string                     `plist:"fileName"`
	TimeStarted       float64                    `plist:"timeStartedRecording"`
	TimeStopped       float64                    `plist:"timeStoppedRecording"`
	PrimaryObservable manifestLogPrimaryObserver `plist:"primaryObservable"`
}

type manifestLogPrimaryObserver struct {
	HighLevelStatus string  `plist:"highLevelStatus"`
	TimeStopped     float64 `plist:"timeStoppedRecording"`
}

type manifestFile struct {
	Logs map[string]manifestLogEntry `plist:"logs"`
}

func loadManifestEntries(path string) ([]manifestLogEntry, error) {
	f, err := os.Open(path) //nolint:gosec // caller-supplied DD path
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var raw manifestFile
	if err := plist.NewDecoder(f).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	out := make([]manifestLogEntry, 0, len(raw.Logs))
	for id, entry := range raw.Logs {
		if entry.UniqueIdentifier == "" {
			entry.UniqueIdentifier = id
		}

		if entry.HighLevelStatus == "" {
			entry.HighLevelStatus = entry.PrimaryObservable.HighLevelStatus
		}

		if entry.TimeStopped == 0 {
			entry.TimeStopped = entry.PrimaryObservable.TimeStopped
		}

		out = append(out, entry)
	}

	return out, nil
}
