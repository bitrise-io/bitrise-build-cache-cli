//go:build unit

package wrapperless

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sealedManifestFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>logs</key>
  <dict>
    <key>AAAA-BBBB</key>
    <dict>
      <key>uniqueIdentifier</key><string>AAAA-BBBB</string>
      <key>signature</key><string>Build MyScheme project</string>
      <key>highLevelStatus</key><string>S</string>
      <key>timeStartedRecording</key><real>762345678.5</real>
      <key>timeStoppedRecording</key><real>762345688.75</real>
    </dict>
    <key>XXXX-OPEN</key>
    <dict>
      <key>uniqueIdentifier</key><string>XXXX-OPEN</string>
      <key>signature</key><string>Build Running project</string>
      <key>timeStartedRecording</key><real>762345690.0</real>
    </dict>
  </dict>
</dict>
</plist>
`

func manifestPath(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "Logs", "Build")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	return filepath.Join(dir, "LogStoreManifest.plist")
}

func writeManifest(t *testing.T, root, body string) string {
	t.Helper()
	path := manifestPath(t, root)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

func waitForEnd(t *testing.T, ch chan End) End {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no End emitted")
		return End{}
	}
}

func TestEndWatcher_EmitsForSealedEntry(t *testing.T) {
	fakeHome := t.TempDir()
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")

	src := newFakeEventSource()
	ends := make(chan End, 4)

	w := &EndWatcher{
		HomeDir:   fakeHome,
		OnEnd:     func(e End) { ends <- e },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// Write manifest AFTER Start so the seed walk doesn't pre-fire.
	path := writeManifest(t, root, sealedManifestFixture)
	src.events <- Event{Path: path, Op: EventWrite}

	got := waitForEnd(t, ends)
	assert.Equal(t, "AAAA-BBBB", got.UniqueIdentifier)
	assert.Equal(t, "MyScheme", got.Scheme)
	assert.True(t, got.Success)
	assert.Greater(t, got.Duration, time.Duration(0))

	w.Close()
}

func TestEndWatcher_DedupesAcrossWriteEvents(t *testing.T) {
	fakeHome := t.TempDir()
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")

	src := newFakeEventSource()
	ends := make(chan End, 4)

	w := &EndWatcher{
		HomeDir:   fakeHome,
		OnEnd:     func(e End) { ends <- e },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	path := writeManifest(t, root, sealedManifestFixture)
	src.events <- Event{Path: path, Op: EventWrite}
	_ = waitForEnd(t, ends)

	src.events <- Event{Path: path, Op: EventWrite}

	select {
	case e := <-ends:
		t.Fatalf("duplicate End emitted: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}

	w.Close()
}

func TestEndWatcher_IgnoresUnsealedEntries(t *testing.T) {
	const onlyOpen = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>logs</key><dict>
  <key>XXXX-OPEN</key>
  <dict>
    <key>uniqueIdentifier</key><string>XXXX-OPEN</string>
    <key>signature</key><string>Build Running project</string>
    <key>timeStartedRecording</key><real>762345690.0</real>
  </dict>
</dict></dict></plist>`

	fakeHome := t.TempDir()
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")

	src := newFakeEventSource()
	ends := make(chan End, 4)

	w := &EndWatcher{
		HomeDir:   fakeHome,
		OnEnd:     func(e End) { ends <- e },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	path := writeManifest(t, root, onlyOpen)
	src.events <- Event{Path: path, Op: EventWrite}

	select {
	case e := <-ends:
		t.Fatalf("unexpected End for open entry: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}

	w.Close()
}

func TestEndWatcher_FailureStatus(t *testing.T) {
	const failedBody = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>logs</key><dict>
  <key>FAIL-01</key>
  <dict>
    <key>uniqueIdentifier</key><string>FAIL-01</string>
    <key>signature</key><string>Build Failing project</string>
    <key>highLevelStatus</key><string>E</string>
    <key>timeStartedRecording</key><real>762345678.0</real>
    <key>timeStoppedRecording</key><real>762345680.0</real>
  </dict>
</dict></dict></plist>`

	fakeHome := t.TempDir()
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")

	src := newFakeEventSource()
	ends := make(chan End, 4)

	w := &EndWatcher{
		HomeDir:   fakeHome,
		OnEnd:     func(e End) { ends <- e },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	path := writeManifest(t, root, failedBody)
	src.events <- Event{Path: path, Op: EventWrite}

	got := waitForEnd(t, ends)
	assert.False(t, got.Success)
	assert.Equal(t, "Failing", got.Scheme)

	w.Close()
}

// TestEndWatcher_SeedPicksUpPreExistingSealedManifest covers the window where
// Xcode sealed a manifest before the proxy's EndWatcher started. The seed
// walk must surface the sealed entry on Start.
func TestEndWatcher_SeedPicksUpPreExistingSealedManifest(t *testing.T) {
	fakeHome := t.TempDir()
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-preexisting")
	// Write manifest BEFORE Start so the seed walk is the only path that
	// can surface it.
	_ = writeManifest(t, root, sealedManifestFixture)

	src := newFakeEventSource()
	ends := make(chan End, 4)

	w := &EndWatcher{
		HomeDir:   fakeHome,
		OnEnd:     func(e End) { ends <- e },
		NewSource: func() (DirEventSource, error) { return src, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	got := waitForEnd(t, ends)
	assert.Equal(t, "AAAA-BBBB", got.UniqueIdentifier)

	w.Close()
}
