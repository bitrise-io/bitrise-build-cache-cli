//go:build unit

package wrapperless_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless"
)

func statInode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	sys, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok, "stat Sys() not syscall.Stat_t on this platform")

	return sys.Ino
}

func TestHandledManifestStore_CompactDropsStaleRecordsFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handled.ndjson")
	store := &wrapperless.HandledManifestStore{Path: path}

	now := time.Now()
	fresh := wrapperless.HandledManifest{UUID: "FRESH", HandledAt: now.Add(-time.Hour)}
	stale := wrapperless.HandledManifest{UUID: "STALE", HandledAt: now.Add(-14 * 24 * time.Hour)}

	require.NoError(t, store.Append(stale))
	require.NoError(t, store.Append(fresh))

	require.NoError(t, store.Compact(now, 7*24*time.Hour))

	body, err := os.ReadFile(path)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	assert.Len(t, lines, 1, "stale record must be removed from disk")
	assert.Contains(t, lines[0], `"uuid":"FRESH"`)
}

func TestHandledManifestStore_CompactNoopWhenAllFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handled.ndjson")
	store := &wrapperless.HandledManifestStore{Path: path}

	now := time.Now()
	for _, u := range []string{"A", "B", "C"} {
		require.NoError(t, store.Append(wrapperless.HandledManifest{UUID: u, HandledAt: now.Add(-time.Hour)}))
	}

	before, err := os.Stat(path)
	require.NoError(t, err)
	beforeIno := statInode(t, path)

	// Sleep long enough that the all-fresh short-circuit's absence would
	// produce a different mtime via the tmp + rename path.
	time.Sleep(10 * time.Millisecond)

	require.NoError(t, store.Compact(now, 7*24*time.Hour))

	after, err := os.Stat(path)
	require.NoError(t, err)
	afterIno := statInode(t, path)

	assert.Equal(t, before.ModTime(), after.ModTime(), "all-fresh ledger must not be rewritten")
	assert.Equal(t, beforeIno, afterIno, "all-fresh ledger inode must not change")
}

func TestHandledManifestStore_CompactMissingFileIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handled.ndjson")
	store := &wrapperless.HandledManifestStore{Path: path}

	require.NoError(t, store.Compact(time.Now(), 7*24*time.Hour))

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "missing ledger must not be created on compact")
}
