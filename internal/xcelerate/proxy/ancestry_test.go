//go:build unit

package proxy

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAncestryCache_EvictsLeastRecentlyUsedOverCapacity(t *testing.T) {
	c := newAncestryCache()

	const overflow = 200
	total := ancestryCacheMax + overflow

	for i := 0; i < total; i++ {
		key := ancestryKey{pid: i, startTime: int64(i)}
		c.put(key, []AncestryEntry{{PID: i, Name: "proc-" + strconv.Itoa(i), StartTimeMS: int64(i)}})
	}

	assert.Equal(t, ancestryCacheMax, c.order.Len(), "list length is capped at ancestryCacheMax")
	assert.Len(t, c.index, ancestryCacheMax, "index is capped at ancestryCacheMax")

	for i := 0; i < overflow; i++ {
		_, ok := c.get(ancestryKey{pid: i, startTime: int64(i)})
		assert.False(t, ok, "entry %d should have been evicted", i)
	}

	for i := overflow; i < total; i++ {
		chain, ok := c.get(ancestryKey{pid: i, startTime: int64(i)})
		assert.True(t, ok, "entry %d should still be cached", i)
		assert.Equal(t, []AncestryEntry{{PID: i, Name: "proc-" + strconv.Itoa(i), StartTimeMS: int64(i)}}, chain)
	}
}

func TestAncestryCache_GetMovesEntryToFrontAndStopsEviction(t *testing.T) {
	c := newAncestryCache()

	for i := 0; i < ancestryCacheMax; i++ {
		c.put(ancestryKey{pid: i, startTime: int64(i)}, []AncestryEntry{{PID: i, Name: strconv.Itoa(i)}})
	}

	// Touch entry 0 so it moves to the front.
	_, ok := c.get(ancestryKey{pid: 0, startTime: 0})
	assert.True(t, ok)

	// Insert one more — entry 1 is now the LRU and should be evicted, not 0.
	c.put(ancestryKey{pid: ancestryCacheMax, startTime: int64(ancestryCacheMax)}, []AncestryEntry{{Name: "new"}})

	_, ok = c.get(ancestryKey{pid: 0, startTime: 0})
	assert.True(t, ok, "entry 0 was recently touched; should survive")

	_, ok = c.get(ancestryKey{pid: 1, startTime: 1})
	assert.False(t, ok, "entry 1 was the LRU after touching 0; should be evicted")
}

func TestToNames_ProjectsNamesInOrder(t *testing.T) {
	in := []AncestryEntry{
		{PID: 1, Name: "swiftc", StartTimeMS: 1},
		{PID: 2, Name: "xcodebuild", StartTimeMS: 2},
	}

	assert.Equal(t, []string{"swiftc", "xcodebuild"}, toNames(in))
	assert.Nil(t, toNames(nil))
}
