//go:build unit

package urllog

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriter_AppendReadDeleteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invocation-urls-1234.ndjson")
	w := &Writer{Path: path}

	require.NoError(t, w.Append("id-a"))
	require.NoError(t, w.Append("id-b"))

	recs, err := Read(path)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, "id-a", recs[0].InvocationID)
	assert.Equal(t, "id-b", recs[1].InvocationID)
	assert.False(t, recs[0].EmittedAt.IsZero())

	require.NoError(t, Delete(path))

	recs, err = Read(path)
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestRead_MissingFileIsEmpty(t *testing.T) {
	recs, err := Read(filepath.Join(t.TempDir(), "nope.ndjson"))
	require.NoError(t, err)
	assert.Nil(t, recs)
}

func TestDelete_MissingFileIsNoop(t *testing.T) {
	require.NoError(t, Delete(filepath.Join(t.TempDir(), "nope.ndjson")))
}

func TestWriter_ConcurrentAppendsPersistAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invocation-urls-5555.ndjson")
	w := &Writer{Path: path}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			_ = w.Append(idOf(id))
		}(i)
	}
	wg.Wait()

	recs, err := Read(path)
	require.NoError(t, err)
	require.Len(t, recs, n)

	seen := make(map[string]struct{}, n)
	for _, r := range recs {
		seen[r.InvocationID] = struct{}{}
	}
	assert.Len(t, seen, n, "every concurrent append must produce a distinct, well-formed line")
}

func idOf(i int) string {
	return "concurrent-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i/26))
}
