package workspace

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"go/token"
	"os"
	"sync"
)

const (
	defaultSourceCacheEntries = 512
	defaultSourceCacheBytes   = 16 << 20
)

type sourceCacheKey struct {
	path   string
	digest [sha256.Size]byte
}

type sourceCacheEntry struct {
	key     sourceCacheKey
	outline *FileOutline
	charge  int64
}

// SourceCacheStats exposes bounded, aggregate diagnostics without leaking file
// names or source content into telemetry.
type SourceCacheStats struct {
	Hits       uint64
	Misses     uint64
	Evictions  uint64
	Entries    int
	Bytes      int64
	MaxEntries int
	MaxBytes   int64
}

// sourceCache is an in-memory LRU of immutable structural parse results.
// Every lookup hashes freshly read bytes. This deliberately trades a file read
// for correctness: same-size, same-mtime external edits cannot return stale
// symbols, and no mutation hook has to predict what an arbitrary shell changed.
type sourceCache struct {
	mu         sync.Mutex
	maxEntries int
	maxBytes   int64
	bytes      int64
	hits       uint64
	misses     uint64
	evictions  uint64
	lru        *list.List
	entries    map[sourceCacheKey]*list.Element
	latest     map[string]sourceCacheKey
	root       string
}

func newSourceCache(maxEntries int, maxBytes int64, roots ...string) *sourceCache {
	if maxEntries <= 0 {
		maxEntries = defaultSourceCacheEntries
	}
	if maxBytes <= 0 {
		maxBytes = defaultSourceCacheBytes
	}
	cache := &sourceCache{
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		lru:        list.New(),
		entries:    make(map[sourceCacheKey]*list.Element),
		latest:     make(map[string]sourceCacheKey),
	}
	if len(roots) > 0 {
		cache.root = roots[0]
	}
	return cache
}

func (c *sourceCache) outline(path string) (*FileOutline, error) {
	var source []byte
	var err error
	if c.root == "" {
		source, err = os.ReadFile(path)
	} else {
		source, err = safeReadTargetLimit(c.root, path, MaxReadBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("codeindex: read file %s: %w", path, err)
	}
	key := sourceCacheKey{path: path, digest: sha256.Sum256(source)}

	if outline, ok := c.get(key); ok {
		return outline, nil
	}

	outline, err := ParseFileOutlineSource(token.NewFileSet(), path, source)
	if err != nil && outline == nil {
		return nil, err
	}
	if outline != nil {
		c.put(key, outline, int64(len(source)))
	}
	return cloneFileOutline(outline), err
}

func (c *sourceCache) get(key sourceCacheKey) (*FileOutline, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}
	c.hits++
	c.lru.MoveToFront(element)
	return cloneFileOutline(element.Value.(*sourceCacheEntry).outline), true
}

func (c *sourceCache) put(key sourceCacheKey, outline *FileOutline, charge int64) {
	if outline == nil || charge > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.entries[key]; ok {
		c.lru.MoveToFront(element)
		return
	}
	if previous, ok := c.latest[key.path]; ok && previous != key {
		c.remove(previous)
	}
	c.insert(key, outline, charge)
	c.evictOverflow()
}

func (c *sourceCache) insert(key sourceCacheKey, outline *FileOutline, charge int64) {
	entry := &sourceCacheEntry{key: key, outline: cloneFileOutline(outline), charge: charge}
	c.entries[key] = c.lru.PushFront(entry)
	c.latest[key.path] = key
	c.bytes += charge
}

func (c *sourceCache) evictOverflow() {
	for len(c.entries) > c.maxEntries || c.bytes > c.maxBytes {
		oldest := c.lru.Back()
		c.remove(oldest.Value.(*sourceCacheEntry).key)
		c.evictions++
	}
}

func (c *sourceCache) remove(key sourceCacheKey) {
	element, ok := c.entries[key]
	if !ok {
		return
	}
	entry := element.Value.(*sourceCacheEntry)
	delete(c.entries, key)
	if c.latest[entry.key.path] == entry.key {
		delete(c.latest, entry.key.path)
	}
	c.bytes -= entry.charge
	c.lru.Remove(element)
}

func (c *sourceCache) stats() SourceCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return SourceCacheStats{
		Hits:       c.hits,
		Misses:     c.misses,
		Evictions:  c.evictions,
		Entries:    len(c.entries),
		Bytes:      c.bytes,
		MaxEntries: c.maxEntries,
		MaxBytes:   c.maxBytes,
	}
}

func cloneFileOutline(outline *FileOutline) *FileOutline {
	if outline == nil {
		return nil
	}
	cloned := *outline
	cloned.Imports = append([]string(nil), outline.Imports...)
	cloned.Symbols = append([]SymbolEntry(nil), outline.Symbols...)
	cloned.Errors = append([]string(nil), outline.Errors...)
	return &cloned
}
