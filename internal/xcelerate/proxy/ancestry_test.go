//go:build unit

package proxy

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAncestryCache_EvictsLeastRecentlyUsedOverCapacity(t *testing.T) {
	c := newAncestryCache()

	// Insert cap+overflow entries. The first `overflow` should get evicted.
	const overflow = 200
	total := ancestryCacheMax + overflow

	for i := 0; i < total; i++ {
		key := ancestryKey{pid: i, startTime: int64(i)}
		c.put(key, []string{"proc-" + strconv.Itoa(i)})
	}

	assert.Equal(t, ancestryCacheMax, c.order.Len(), "list length is capped at ancestryCacheMax")
	assert.Len(t, c.index, ancestryCacheMax, "index is capped at ancestryCacheMax")

	// Earliest inserts fall out.
	for i := 0; i < overflow; i++ {
		_, ok := c.get(ancestryKey{pid: i, startTime: int64(i)})
		assert.False(t, ok, "entry %d should have been evicted", i)
	}

	// Most recent inserts survive.
	for i := overflow; i < total; i++ {
		chain, ok := c.get(ancestryKey{pid: i, startTime: int64(i)})
		assert.True(t, ok, "entry %d should still be cached", i)
		assert.Equal(t, []string{"proc-" + strconv.Itoa(i)}, chain)
	}
}

func TestAncestryCache_GetMovesEntryToFrontAndStopsEviction(t *testing.T) {
	c := newAncestryCache()

	// Fill to capacity.
	for i := 0; i < ancestryCacheMax; i++ {
		c.put(ancestryKey{pid: i, startTime: int64(i)}, []string{strconv.Itoa(i)})
	}

	// Touch entry 0 so it moves to the front.
	_, ok := c.get(ancestryKey{pid: 0, startTime: 0})
	assert.True(t, ok)

	// Insert one more — entry 1 is now the LRU and should be evicted, not 0.
	c.put(ancestryKey{pid: ancestryCacheMax, startTime: int64(ancestryCacheMax)}, []string{"new"})

	_, ok = c.get(ancestryKey{pid: 0, startTime: 0})
	assert.True(t, ok, "entry 0 was recently touched; should survive")

	_, ok = c.get(ancestryKey{pid: 1, startTime: 1})
	assert.False(t, ok, "entry 1 was the LRU after touching 0; should be evicted")
}
