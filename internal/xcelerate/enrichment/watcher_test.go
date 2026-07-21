//go:build unit

package enrichment_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestWatcher_EmitsOnlyNewEntries(t *testing.T) {
	home := t.TempDir()
	derived := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc/Logs/Build")
	require.NoError(t, os.MkdirAll(derived, 0o755))

	fixture, err := os.ReadFile("testdata/LogStoreManifest.plist")
	require.NoError(t, err)

	manifestPath := filepath.Join(derived, "LogStoreManifest.plist")
	require.NoError(t, os.WriteFile(manifestPath, fixture, 0o644))

	var (
		mu        sync.Mutex
		collected []string
	)

	// Fixture timestamps are Cocoa reference date + 762345688 ≈ 2025-02-27; pin Now so the age gate treats those entries as recent.
	fixtureNow := time.Date(2025, 2, 27, 12, 0, 0, 0, time.UTC)

	w := &enrichment.Watcher{
		HomeDir:      home,
		PollInterval: 20 * time.Millisecond,
		Now:          func() time.Time { return fixtureNow },
		Handle: func(e enrichment.ManifestEntry) {
			mu.Lock()
			defer mu.Unlock()
			collected = append(collected, e.UUID)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Seed pass consumed both existing entries — nothing should fire yet.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Empty(t, collected, "seed scan must not emit historic entries")
	mu.Unlock()

	// Introduce a new manifest under a different DerivedData folder.
	other := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-xyz/Logs/Build")
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(other, "LogStoreManifest.plist"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
		<key>NEW-BUILD-UUID</key>
		<dict>
			<key>fileName</key><string>NEW-BUILD-UUID.xcactivitylog</string>
			<key>highLevelStatus</key><string>S</string>
			<key>signature</key><string>Build FreshScheme</string>
			<key>timeStartedRecording</key><real>762345900.0</real>
			<key>timeStoppedRecording</key><real>762345910.0</real>
		</dict>
	</dict>
</dict>
</plist>`), 0o644))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := append([]string(nil), collected...)
		mu.Unlock()

		if len(got) == 1 {
			assert.Equal(t, "NEW-BUILD-UUID", got[0])

			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, collected, 1, "watcher should emit exactly one new entry")
}
