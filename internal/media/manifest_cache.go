package media

import (
	"sync"
	"time"
)

const (
	// deviceManifestTTL is how long a device manifest URL is reused. The URL is
	// an unsigned CDN path with no expiry parameter, so the bound is not a
	// lifetime: it keeps a replaced asset from being served for long, and a
	// failed fetch drops the entry sooner (forget).
	deviceManifestTTL = 12 * time.Hour
	// deviceManifestCacheCap bounds the cache. One entry is two short strings.
	deviceManifestCacheCap = 1024
)

type deviceManifestKey struct {
	storefront string
	songID     string
}

type deviceManifestEntry struct {
	url     string
	expires time.Time
}

// deviceManifestCache remembers wrapper.M3U8 answers per (storefront, song).
// The wrapper serves /m3u8 and /key from one lock and caches nothing, so every
// repeat is a request other downloads queue behind. One cache is shared by all
// job-scoped Downloader clones; a nil cache stores nothing.
type deviceManifestCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[deviceManifestKey]deviceManifestEntry
}

func newDeviceManifestCache() *deviceManifestCache {
	return &deviceManifestCache{now: time.Now, entries: map[deviceManifestKey]deviceManifestEntry{}}
}

func (c *deviceManifestCache) get(key deviceManifestKey) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if !c.now().Before(entry.expires) {
		delete(c.entries, key)
		return "", false
	}
	return entry.url, true
}

func (c *deviceManifestCache) put(key deviceManifestKey, url string) {
	if c == nil || url == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, replacing := c.entries[key]; !replacing && len(c.entries) >= deviceManifestCacheCap {
		c.evictLocked(now)
	}
	c.entries[key] = deviceManifestEntry{url: url, expires: now.Add(deviceManifestTTL)}
}

// evictLocked makes room for one entry: it drops every expired entry, and if
// that frees nothing, the entry closest to expiry (the oldest, since every entry
// lives the same TTL).
func (c *deviceManifestCache) evictLocked(now time.Time) {
	var oldest deviceManifestKey
	var oldestExpires time.Time
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
			continue
		}
		if oldestExpires.IsZero() || entry.expires.Before(oldestExpires) {
			oldest, oldestExpires = key, entry.expires
		}
	}
	if len(c.entries) >= deviceManifestCacheCap {
		delete(c.entries, oldest)
	}
}

// forget drops key, for a cached URL that no longer leads to a playlist.
func (c *deviceManifestCache) forget(key deviceManifestKey) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
