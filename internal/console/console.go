// Package console serves the embedded web console: a no-build Preact+htm
// single-page app that reads only the public /api/v1 API. The files under
// static/ are exactly what the browser runs; see AGENTS.md and DESIGN.md in
// this directory.
package console

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

//go:embed all:static
var embedded embed.FS

// ContentSecurityPolicy is sent with every console response. The app loads
// scripts and styles only from its own origin, artwork from Apple's CDNs
// (https:), and motion artwork as HLS (https:, blob:). Inline scripts and
// style attributes are blocked, so the app sets dynamic colors through the
// CSSOM only.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' https: data:; media-src 'self' https: blob:; connect-src 'self'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

const (
	indexFile = "index.html"
	// vendorDir holds third-party files whose names carry their version, so
	// their bytes never change under a name and browsers may cache them forever.
	vendorDir = "vendor/"
)

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".svg":  "image/svg+xml",
	".json": "application/json",
	".png":  "image/png",
	".ico":  "image/x-icon",
	".txt":  "text/plain; charset=utf-8",
}

type asset struct {
	body        []byte
	etag        string
	contentType string
	immutable   bool
}

var (
	assetsOnce sync.Once
	assets     map[string]asset
	assetsErr  error
)

// loadAssets reads every embedded file once. Embedded files report a zero
// ModTime, so http.FileServer could send neither Last-Modified nor ETag;
// a content hash gives every file a strong ETag instead.
func loadAssets() (map[string]asset, error) {
	assetsOnce.Do(func() {
		root, err := fs.Sub(embedded, "static")
		if err != nil {
			assetsErr = fmt.Errorf("console: open embedded static dir: %w", err)
			return
		}
		out := map[string]asset{}
		assetsErr = fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, err := fs.ReadFile(root, name)
			if err != nil {
				return fmt.Errorf("console: read embedded %s: %w", name, err)
			}
			sum := sha256.Sum256(body)
			ctype, ok := contentTypes[path.Ext(name)]
			if !ok {
				ctype = "application/octet-stream"
			}
			out[name] = asset{
				body:        body,
				etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
				contentType: ctype,
				immutable:   strings.HasPrefix(name, vendorDir),
			}
			return nil
		})
		assets = out
	})
	return assets, assetsErr
}

// Handler serves the console's embedded files. It expects request paths
// relative to the console root, so mount it behind http.StripPrefix: "/"
// and "" serve index.html, "/css/base.css" serves static/css/base.css.
// Directories are never listed. Every response, including 304 and 404,
// carries the Content-Security-Policy and the other security headers.
func Handler() http.Handler {
	return http.HandlerFunc(serve)
}

func serve(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", ContentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	files, err := loadAssets()
	if err != nil {
		http.Error(w, "console assets unavailable", http.StatusInternalServerError)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = indexFile
	}
	a, ok := files[name]
	if !ok || !fs.ValidPath(name) {
		h.Set("Cache-Control", "no-store")
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.Set("Content-Type", a.contentType)
	h.Set("ETag", a.etag)
	if a.immutable {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(a.body))
}
