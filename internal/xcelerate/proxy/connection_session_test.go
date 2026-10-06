//go:build unit

package proxy

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionRegistry_PutGetForget(t *testing.T) {
	r := newSessionRegistry()

	cs1 := newConnectionSession(100, time.Now())
	cs2 := newConnectionSession(200, time.Now())

	r.put("k1", cs1)
	r.put("k2", cs2)

	assert.Equal(t, 2, registryLen(r))

	k, ok := r.keyFor(cs1)
	assert.True(t, ok)
	assert.Equal(t, "k1", k)

	r.forget("k1")
	assert.Equal(t, 1, registryLen(r))

	_, ok = r.keyFor(cs1)
	assert.False(t, ok)
}

func TestSessionRegistry_ConcurrentPut(t *testing.T) {
	r := newSessionRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			r.put(keyAt(i), newConnectionSession(i, time.Now()))
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, registryLen(r))
}

// registryLen walks the registry via flushAll — the private byKey map is
// package-local so this is only usable from this test file.
func registryLen(r *sessionRegistry) int {
	n := 0
	r.flushAll(func(*connectionSession) { n++ })

	return n
}

func TestSessionRegistry_FlushAllInvokesEveryOutstandingSession(t *testing.T) {
	r := newSessionRegistry()

	for i := 0; i < 5; i++ {
		r.put(keyAt(i), newConnectionSession(i, time.Now()))
	}

	var mu sync.Mutex
	seen := make(map[*connectionSession]struct{})

	r.flushAll(func(cs *connectionSession) {
		mu.Lock()
		defer mu.Unlock()

		seen[cs] = struct{}{}
	})

	assert.Len(t, seen, 5)
}

func keyAt(i int) string {
	return "k-" + string(rune('A'+i))
}

func TestFlushSession_SkipsWhenConnectionHadNoActivity(t *testing.T) {
	dir := t.TempDir()
	h := &sidecarStatsHandler{
		writer:           newSidecarWriter(dir, nil),
		ancestryResolver: func(int) []string { return nil },
	}

	cs := newConnectionSession(42, time.Now())
	h.flushSession(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "zero-RPC connection must not produce a sidecar file")
}

func TestFlushSession_WritesWhenConnectionHadActivity(t *testing.T) {
	dir := t.TempDir()
	h := &sidecarStatsHandler{
		writer:           newSidecarWriter(dir, nil),
		ancestryResolver: func(int) []string { return nil },
	}

	cs := newConnectionSession(42, time.Now())
	cs.markActivity()
	h.flushSession(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "a conn that recorded an RPC must emit a sidecar")
}
