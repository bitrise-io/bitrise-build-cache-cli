//go:build unit

package recwatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func drain(t *testing.T, ch <-chan Event, want string) Event {
	t.Helper()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed before event for %s", want)
			}

			if ev.Path == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("no event for %s within deadline", want)
		}
	}
}

func drainContains(t *testing.T, ch <-chan Event, want string) Event {
	t.Helper()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed before event matching %s", want)
			}

			if filepath.Base(ev.Path) == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("no event containing %s within deadline", want)
		}
	}
}

func TestWatcher_EmitsCreateForFileLandingInSubscribedDir(t *testing.T) {
	root := t.TempDir()

	w, err := New(Create | Write)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.Add(root))

	file := filepath.Join(root, "landed.txt")
	require.NoError(t, os.WriteFile(file, []byte("hi"), 0o644))

	ev := drain(t, w.Events(), file)
	assert.NotZero(t, ev.Op&Create)
}

func TestWatcher_AtomicDeepMkdirReplaysDescendants(t *testing.T) {
	root := t.TempDir()

	w, err := New(Create)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.Add(root))

	// mkdir -p a/b/c + file.txt inside, as one go: our walk-on-create must
	// surface the whole subtree via synthetic events.
	deepFile := filepath.Join(root, "a", "b", "c", "file.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(deepFile), 0o755))
	require.NoError(t, os.WriteFile(deepFile, []byte("x"), 0o644))

	drainContains(t, w.Events(), "file.txt")
}

func TestWatcher_SubscribesToDirCreatedAfterAdd(t *testing.T) {
	root := t.TempDir()

	w, err := New(Create | Write)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.Add(root))

	sub := filepath.Join(root, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	drain(t, w.Events(), sub)

	// Write a file inside the new sub-dir; our CREATE-handler must have
	// installed a watch on it by now.
	file := filepath.Join(sub, "later.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	drain(t, w.Events(), file)
}

func TestWatcher_CreatesMissingRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "not-yet")

	w, err := New(Create)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.Add(root))

	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	file := filepath.Join(root, "x.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	drain(t, w.Events(), file)
}

func TestWatcher_FiltersByOps(t *testing.T) {
	root := t.TempDir()

	w, err := New(Write)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.Add(root))

	// CREATE-only event should NOT surface because only Write subscribed.
	sub := filepath.Join(root, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	select {
	case ev := <-w.Events():
		if ev.Op&Write != 0 {
			return // write arrived (atime touch), allowed
		}

		t.Fatalf("unexpected non-Write event: %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWatcher_CloseIdempotent(t *testing.T) {
	w, err := New(Create)
	require.NoError(t, err)

	w.Close()
	w.Close()
}

