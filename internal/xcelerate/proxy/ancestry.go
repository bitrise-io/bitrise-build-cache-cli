package proxy

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

// ancestryCache is a bounded LRU keyed by (pid, process-start-time) so a
// recycled PID can't return a stale chain. CreateTime (ms precision) is
// enough to disambiguate PID reuse within one build.
type ancestryCache struct {
	mu    sync.Mutex
	index map[ancestryKey]*list.Element
	order *list.List // front = most recently used
}

type ancestryKey struct {
	pid       int
	startTime int64
}

type ancestryEntry struct {
	key   ancestryKey
	chain []string
}

func newAncestryCache() *ancestryCache {
	return &ancestryCache{
		index: make(map[ancestryKey]*list.Element),
		order: list.New(),
	}
}

func (c *ancestryCache) get(k ancestryKey) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.index[k]
	if !ok {
		return nil, false
	}

	c.order.MoveToFront(el)

	//nolint:forcetypeassert // elements are always *ancestryEntry
	return el.Value.(*ancestryEntry).chain, true
}

func (c *ancestryCache) put(k ancestryKey, chain []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.index[k]; ok {
		c.order.MoveToFront(el)
		//nolint:forcetypeassert // elements are always *ancestryEntry
		el.Value.(*ancestryEntry).chain = chain

		return
	}

	el := c.order.PushFront(&ancestryEntry{key: k, chain: chain})
	c.index[k] = el

	for c.order.Len() > ancestryCacheMax {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}

		//nolint:forcetypeassert // elements are always *ancestryEntry
		delete(c.index, oldest.Value.(*ancestryEntry).key)
		c.order.Remove(oldest)
	}
}

// AncestryEntry mirrors buildidentity.AncestryEntry: one hop up the chain.
// Defined here to avoid pulling buildidentity into the sidecar writer path.
type AncestryEntry struct {
	PID         int
	Name        string
	StartTimeMS int64
}

// resolveAncestryEntries is the rich variant of resolveAncestry; walks parent
// PIDs returning (pid, name, start-ms) triples deepest-first. Separate from
// resolveAncestry so the sidecar writer's []string signature stays stable.
func (c *ancestryCache) resolveAncestryEntries(pid int) []AncestryEntry {
	if pid <= 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), ancestryLookupLimit)
	defer cancel()

	return walkAncestryEntries(ctx, pid)
}

func walkAncestryEntries(ctx context.Context, pid int) []AncestryEntry {
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

// resolveAncestry walks parent PIDs up to ancestryMaxHops and returns executable
// basenames deepest-first (caller process → ancestors). Hard-capped on hops and
// wall-clock. Zero pid returns nil.
func (c *ancestryCache) resolveAncestry(pid int) []string {
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

	chain := walkAncestry(ctx, pid)
	c.put(key, chain)

	return chain
}

func walkAncestry(ctx context.Context, pid int) []string {
	chain := make([]string, 0, 8) //nolint:mnd // typical compile chain depth

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

		chain = append(chain, name)

		ppid, err := proc.PpidWithContext(ctx)
		if err != nil || ppid <= 1 || ppid == current {
			return chain
		}

		current = ppid
	}

	return chain
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
