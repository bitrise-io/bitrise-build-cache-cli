package buildidentity

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

const (
	ancestryMaxHops     = 32
	ancestryCacheMax    = 1000
	ancestryLookupLimit = 500 * time.Millisecond
)

// AncestryCache is a bounded LRU keyed by (pid, process-start-time) so a
// recycled PID can't return a stale chain. CreateTime (ms precision) is
// enough to disambiguate PID reuse within one build.
type AncestryCache struct {
	mu    sync.Mutex
	index map[ancestryKey]*list.Element
	order *list.List // front = most recently used
}

type ancestryKey struct {
	pid       int
	startTime int64
}

type ancestryCacheEntry struct {
	key   ancestryKey
	chain []AncestryEntry
}

// NewAncestryCache returns an empty cache sized to ancestryCacheMax entries.
func NewAncestryCache() *AncestryCache {
	return &AncestryCache{
		index: make(map[ancestryKey]*list.Element),
		order: list.New(),
	}
}

func (c *AncestryCache) get(k ancestryKey) ([]AncestryEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.index[k]
	if !ok {
		return nil, false
	}

	c.order.MoveToFront(el)

	//nolint:forcetypeassert // elements are always *ancestryCacheEntry
	return el.Value.(*ancestryCacheEntry).chain, true
}

func (c *AncestryCache) put(k ancestryKey, chain []AncestryEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.index[k]; ok {
		c.order.MoveToFront(el)
		//nolint:forcetypeassert // elements are always *ancestryCacheEntry
		el.Value.(*ancestryCacheEntry).chain = chain

		return
	}

	el := c.order.PushFront(&ancestryCacheEntry{key: k, chain: chain})
	c.index[k] = el

	for c.order.Len() > ancestryCacheMax {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}

		//nolint:forcetypeassert // elements are always *ancestryCacheEntry
		delete(c.index, oldest.Value.(*ancestryCacheEntry).key)
		c.order.Remove(oldest)
	}
}

// ResolveAncestryEntries walks parent PIDs returning (pid, name, start-ms)
// triples deepest-first. Hard-capped on hops and wall-clock. Zero pid returns
// nil. Cached by (pid, start-time) so a repeat call for the same peer is a
// map lookup.
func (c *AncestryCache) ResolveAncestryEntries(pid int) []AncestryEntry {
	if pid <= 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), ancestryLookupLimit)
	defer cancel()

	//nolint:gosec // PIDs fit in int32 on every OS we target.
	start, err := processStartTime(ctx, int32(pid))
	if err != nil {
		return nil
	}

	key := ancestryKey{pid: pid, startTime: start}
	if chain, ok := c.get(key); ok {
		return chain
	}

	chain := WalkAncestryEntries(ctx, pid)
	c.put(key, chain)

	return chain
}

// WalkAncestryEntries walks parent PIDs from `pid` upward, returning a
// deepest-first chain. Capped on hops and ctx deadline so a pathological
// process tree can never block the caller indefinitely.
func WalkAncestryEntries(ctx context.Context, pid int) []AncestryEntry {
	chain := make([]AncestryEntry, 0, 8) //nolint:mnd // typical compile chain depth

	//nolint:gosec // PIDs fit in int32.
	current := int32(pid)

	for range ancestryMaxHops {
		if ctx.Err() != nil {
			return chain
		}

		proc, err := process.NewProcessWithContext(ctx, current)
		if err != nil {
			return chain
		}

		name, err := proc.NameWithContext(ctx)
		if err != nil || name == "" {
			return chain
		}

		ct, err := proc.CreateTimeWithContext(ctx)
		if err != nil {
			ct = 0
		}

		chain = append(chain, AncestryEntry{PID: int(current), Name: name, StartTimeMS: ct})

		ppid, err := proc.PpidWithContext(ctx)
		if err != nil || ppid <= 1 || ppid == current {
			return chain
		}

		current = ppid
	}

	return chain
}

// WalkAncestryForPID is a convenience helper for callers that want a
// cache-less, fresh ancestry chain for a single PID (e.g. a short-lived shim
// that walks its own ancestry once at startup).
func WalkAncestryForPID(pid int) []AncestryEntry {
	if pid <= 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), ancestryLookupLimit)
	defer cancel()

	return WalkAncestryEntries(ctx, pid)
}

// processStartTime returns the ms-since-epoch start time of pid; zero on error.
func processStartTime(ctx context.Context, pid int32) (int64, error) {
	proc, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return 0, err //nolint:wrapcheck // caller treats as "unresolvable PID"
	}

	ct, err := proc.CreateTimeWithContext(ctx)
	if err != nil {
		return 0, err //nolint:wrapcheck // caller treats as "unresolvable PID"
	}

	return ct, nil
}
