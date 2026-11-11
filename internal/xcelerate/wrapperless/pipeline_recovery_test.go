//go:build unit

package wrapperless_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless"
)

func writeXcbuildData(t *testing.T, root, hex, action, qos string) {
	t.Helper()

	dir := filepath.Join(root, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"),
		[]byte(`{"parameters":{"action":"`+action+`","qos":"`+qos+`"}}`), 0o644))
}

func writeSealedManifest(t *testing.T, root string, uid string) {
	t.Helper()

	dir := filepath.Join(root, "Logs", "Build")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	body := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>logs</key><dict>
  <key>` + uid + `</key>
  <dict>
    <key>uniqueIdentifier</key><string>` + uid + `</string>
    <key>signature</key><string>Build Seek project</string>
    <key>highLevelStatus</key><string>S</string>
    <key>timeStartedRecording</key><real>762345678.0</real>
    <key>timeStoppedRecording</key><real>762345688.0</real>
  </dict>
</dict></dict></plist>`

	require.NoError(t, os.WriteFile(filepath.Join(dir, "LogStoreManifest.plist"), []byte(body), 0o644))
}

func TestPipeline_RecoversSealedPrimary(t *testing.T) {
	fakeHome := t.TempDir()
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")
	writeXcbuildData(t, root, "deadbeefdeadbeefdeadbeefdeadbeef", "build", "")
	writeSealedManifest(t, root, "LOG-UNIQ-1")

	fp := &fakeProxy{}
	putter := &recordingPutter{}

	tl := wrapperless.NewTimeline()
	alloc := wrapperless.NewAllocator("h", 42, 1_000)

	p := &wrapperless.Pipeline{
		HomeDir:   fakeHome,
		Timeline:  tl,
		Allocator: alloc,
		Proxy:     fp,
		Emitter:   &wrapperless.Emitter{Client: putter, DrainInterval: time.Hour},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.Start(ctx))
	t.Cleanup(p.Close)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(putter.snapshot()) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	calls := putter.snapshot()
	require.GreaterOrEqual(t, len(calls), 2, "sealed primary must produce a slim + enriched pair")
}

func TestPipeline_RecoversUnsealedPrimaryStartOnly(t *testing.T) {
	fakeHome := t.TempDir()
	root := filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData", "Seek-abc")
	writeXcbuildData(t, root, "cafebabecafebabecafebabecafebabe", "build", "")

	fp := &fakeProxy{}
	putter := &recordingPutter{}

	tl := wrapperless.NewTimeline()
	alloc := wrapperless.NewAllocator("h", 42, 1_000)

	p := &wrapperless.Pipeline{
		HomeDir:   fakeHome,
		Timeline:  tl,
		Allocator: alloc,
		Proxy:     fp,
		Emitter:   &wrapperless.Emitter{Client: putter, DrainInterval: time.Hour},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.Start(ctx))
	t.Cleanup(p.Close)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(fp.snapshot()) > 0 && len(putter.snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	assert.NotEmpty(t, fp.snapshot(), "unsealed primary must reseat the proxy with an open invocation ID")
	assert.Len(t, putter.snapshot(), 1, "unsealed primary → only slim, no enriched yet")
}
