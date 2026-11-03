package artwork

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// diskLimit is how much the disk cache keeps; the oldest-used covers go
// first. Apple's CDN lets covers be cached for half a year, so a cover is
// fetched once and read from here on later launches.
const diskLimit = 256 << 20

// diskCache keeps fetched cover bytes, one file per URL. A nil *diskCache
// keeps nothing.
type diskCache struct {
	dir string
}

// WithDisk keeps fetched covers in dir across launches and trims dir to
// diskLimit in the background. Call it before the first Get.
func (c *Cache) WithDisk(dir string) *Cache {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Warn("artwork disk cache unavailable", "dir", dir, "err", err)
		return c
	}
	c.disk = &diskCache{dir: dir}
	go c.disk.trim(diskLimit)
	return c
}

func (d *diskCache) path(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(d.dir, hex.EncodeToString(sum[:16]))
}

func (d *diskCache) get(url string) ([]byte, bool) {
	if d == nil {
		return nil, false
	}
	p := d.path(url)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	now := time.Now()
	_ = os.Chtimes(p, now, now) // marks it used for trim
	return data, true
}

func (d *diskCache) put(url string, data []byte) {
	if d == nil {
		return
	}
	tmp, err := os.CreateTemp(d.dir, ".part-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), d.path(url)) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// trim removes the least recently used covers until the cache holds at most
// limit bytes, and leftovers of interrupted writes.
func (d *diskCache) trim(limit int64) {
	type file struct {
		path string
		size int64
		used time.Time
	}
	var files []file
	var total int64
	_ = filepath.WalkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return nil
		}
		if filepath.Base(p)[0] == '.' && time.Since(info.ModTime()) > time.Hour {
			_ = os.Remove(p)
			return nil
		}
		files = append(files, file{p, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].used.Before(files[j].used) })
	for _, f := range files {
		if total <= limit {
			return
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
