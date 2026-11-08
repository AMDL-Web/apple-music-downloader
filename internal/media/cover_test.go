package media

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

func TestTrackCoverURLs(t *testing.T) {
	const songArt, albumArt = "https://example.test/song.jpg", "https://example.test/album.jpg"
	tests := []struct {
		name     string
		song     applemusic.Song
		typ      applemusic.URLType
		wantURLs []string
	}{
		{"playlist prefers the song, then the album", applemusic.Song{ArtworkURL: songArt, AlbumArtworkURL: albumArt}, applemusic.TypePlaylist, []string{songArt, albumArt}},
		{"playlist dedupes the same art", applemusic.Song{ArtworkURL: songArt, AlbumArtworkURL: songArt}, applemusic.TypePlaylist, []string{songArt}},
		{"album uses the song only", applemusic.Song{ArtworkURL: songArt, AlbumArtworkURL: albumArt}, applemusic.TypeAlbum, []string{songArt}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trackCoverURLs(tt.song, tt.typ); !slices.Equal(got, tt.wantURLs) {
				t.Fatalf("trackCoverURLs() = %#v, want %#v", got, tt.wantURLs)
			}
		})
	}
}

// coverCatalog answers FetchCover with fetch and records each request.
func coverCatalog(fetch func(ctx context.Context, url, format, size string) ([]byte, error)) (fakeDownloaderCatalog, *callLog) {
	calls := &callLog{}
	return fakeDownloaderCatalog{cover: fetch, calls: calls}, calls
}

func TestCoverCacheCoalescesConcurrentFetches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		catalog, calls := coverCatalog(func(ctx context.Context, _, _, _ string) ([]byte, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return []byte("cover"), nil
			}
		})
		cache := newCoverCache(catalog)
		type result struct {
			data []byte
			err  error
		}
		results := make(chan result, 2)
		for range 2 {
			go func() {
				data, err := cache.fetch(t.Context(), []string{"album-art"}, "jpg", "5000x5000")
				results <- result{data: data, err: err}
			}()
		}
		synctest.Wait() // both callers are in, one request is out
		if got := calls.count("cover album-art jpg 5000x5000"); got != 1 {
			t.Errorf("cover requests in flight = %d, want 1 shared", got)
		}
		close(release)
		for range 2 {
			if r := <-results; r.err != nil || !bytes.Equal(r.data, []byte("cover")) {
				t.Fatalf("fetch result = %q, %v", r.data, r.err)
			}
		}
		if got := calls.count("cover album-art jpg 5000x5000"); got != 1 {
			t.Fatalf("cover requests = %d, want 1", got)
		}
	})
}

func TestCoverCacheSharesFallbackCandidate(t *testing.T) {
	catalog, calls := coverCatalog(func(_ context.Context, url, _, _ string) ([]byte, error) {
		if url == "track-art" {
			return nil, errors.New("track cover unavailable")
		}
		return []byte("album-cover"), nil
	})
	cache := newCoverCache(catalog)
	data, err := cache.fetch(t.Context(), []string{"track-art", "album-art"}, "jpg", "3000x3000")
	if err != nil || !bytes.Equal(data, []byte("album-cover")) {
		t.Fatalf("fallback fetch = %q, %v", data, err)
	}
	data, err = cache.fetch(t.Context(), []string{"album-art"}, "jpg", "3000x3000")
	if err != nil || !bytes.Equal(data, []byte("album-cover")) {
		t.Fatalf("cached album fetch = %q, %v", data, err)
	}
	if got := calls.list(); !slices.Equal(got, []string{"cover track-art jpg 3000x3000", "cover album-art jpg 3000x3000"}) {
		t.Fatalf("cover requests = %v, want one per candidate", got)
	}
}

func TestCoverCacheSeparatesFormatAndSize(t *testing.T) {
	catalog, calls := coverCatalog(func(_ context.Context, _, format, size string) ([]byte, error) {
		return []byte(format + ":" + size), nil
	})
	cache := newCoverCache(catalog)
	requests := [][2]string{{"jpg", "5000x5000"}, {"png", "5000x5000"}, {"jpg", "3000x3000"}}
	for range 2 {
		for _, r := range requests {
			if _, err := cache.fetch(t.Context(), []string{"album-art"}, r[0], r[1]); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, r := range requests {
		if got := calls.count("cover album-art " + r[0] + " " + r[1]); got != 1 {
			t.Fatalf("requests for %v = %d, want 1", r, got)
		}
	}
}

func TestCoverCacheDoesNotRetainFailures(t *testing.T) {
	var attempts int
	catalog, calls := coverCatalog(func(context.Context, string, string, string) ([]byte, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("temporary failure")
		}
		return []byte("recovered"), nil
	})
	cache := newCoverCache(catalog)
	if _, err := cache.fetch(t.Context(), []string{"album-art"}, "jpg", "5000x5000"); err == nil {
		t.Fatal("first fetch succeeded, want temporary failure")
	}
	data, err := cache.fetch(t.Context(), []string{"album-art"}, "jpg", "5000x5000")
	if err != nil || !bytes.Equal(data, []byte("recovered")) {
		t.Fatalf("retry fetch = %q, %v", data, err)
	}
	if got := calls.count("cover album-art jpg 5000x5000"); got != 2 {
		t.Fatalf("cover requests = %d, want 2", got)
	}
}

func TestCoverCacheEvictsLeastRecentlyUsedBytes(t *testing.T) {
	catalog, calls := coverCatalog(func(_ context.Context, url, _, _ string) ([]byte, error) {
		return []byte(url), nil
	})
	cache := newCoverCacheWithLimit(catalog, int64(len("art1")))
	for _, url := range []string{"art1", "art2", "art1"} {
		if _, err := cache.fetch(t.Context(), []string{url}, "jpg", "5000x5000"); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.count("cover art1 jpg 5000x5000"); got != 2 {
		t.Fatalf("art1 requests = %d, want 2 after eviction", got)
	}
	if got := calls.count("cover art2 jpg 5000x5000"); got != 1 {
		t.Fatalf("art2 requests = %d, want 1", got)
	}
}

func TestStandaloneAndEmbeddedCoversShareJobCache(t *testing.T) {
	catalog, calls := coverCatalog(func(context.Context, string, string, string) ([]byte, error) {
		return []byte("shared-cover"), nil
	})
	cfg := config.Default()
	downloader := newTestDownloader(&Downloader{cfg: cfg, catalog: catalog, covers: newCoverCache(catalog)})
	path := filepath.Join(t.TempDir(), "album", "cover.jpg")
	if err := downloader.ensureStandaloneCover(t.Context(), path, func(context.Context) (string, error) {
		return "album-art", nil
	}, false); err != nil {
		t.Fatal(err)
	}
	if written, err := os.ReadFile(path); err != nil || !bytes.Equal(written, []byte("shared-cover")) {
		t.Fatalf("standalone cover = %q, %v", written, err)
	}
	embedded, err := downloader.fetchCover(t.Context(), []string{"album-art"}, cfg.Download.CoverFormat, cfg.Download.CoverSize)
	if err != nil || !bytes.Equal(embedded, []byte("shared-cover")) {
		t.Fatalf("embedded cover = %q, %v", embedded, err)
	}
	if got := len(calls.with("cover ")); got != 1 {
		t.Fatalf("cover requests = %d, want 1 shared request", got)
	}
}

func TestStandaloneCoverFailureIsHandledOncePerJob(t *testing.T) {
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	downloader := newTestDownloader(&Downloader{cfg: cfg, standaloneCoverState: newStandaloneCoverState()})
	path := filepath.Join(t.TempDir(), "artist", "artist.jpg")
	wantErr := errors.New("artist not found")
	var calls int
	resolve := func(context.Context) (string, error) {
		calls++
		return "", wantErr
	}

	if err := downloader.ensureStandaloneCover(t.Context(), path, resolve, false); !errors.Is(err, wantErr) {
		t.Fatalf("first ensure error=%v, want %v", err, wantErr)
	}
	if err := downloader.ensureStandaloneCover(t.Context(), path, resolve, false); err != nil {
		t.Fatalf("second ensure error=%v, want suppressed duplicate", err)
	}
	if calls != 1 {
		t.Fatalf("resolver calls=%d, want 1", calls)
	}
}

func TestStandaloneCoversForDifferentPathsProceedConcurrently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		catalog, calls := coverCatalog(func(ctx context.Context, url, _, _ string) ([]byte, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return []byte(url), nil
			}
		})
		cfg := config.Default()
		cfg.Download.MaxAttempts = 1
		downloader := newTestDownloader(&Downloader{cfg: cfg, catalog: catalog, standaloneCoverState: newStandaloneCoverState()})
		dir := t.TempDir()
		paths := map[string]string{
			"art-a": filepath.Join(dir, "album-a", "cover.jpg"),
			"art-b": filepath.Join(dir, "album-b", "cover.jpg"),
		}
		errs := make(chan error, len(paths))
		for url, path := range paths {
			go func() {
				errs <- downloader.ensureStandaloneCover(t.Context(), path, func(context.Context) (string, error) {
					return url, nil
				}, false)
			}()
		}
		synctest.Wait()
		if got := len(calls.with("cover ")); got != len(paths) {
			t.Errorf("cover fetches in flight = %d, want %d: covers for different paths were serialized", got, len(paths))
		}
		close(release)
		for range paths {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		for url, path := range paths {
			if data, err := os.ReadFile(path); err != nil || string(data) != url {
				t.Fatalf("cover %q = %q, %v", path, data, err)
			}
		}
	})
}

// Standalone covers land where a library album or playlist keeps them, named
// by the configured format: cover.<ext> in the album (or playlist) folder,
// artist.<ext> in the album artist's folder. Only library adds write the
// album and artist covers; a file already there is kept unless the covers
// are reset.
func TestStandaloneCoverPlacement(t *testing.T) {
	album := applemusic.Collection{ID: "100", Name: "Album", Artist: "Artist"}
	song := applemusic.Song{
		ID: "1001", Name: "Song", AlbumID: "100", AlbumName: "Album", AlbumArtist: "Artist", AlbumArtistID: "art-1",
		ArtworkURL: "song-art", AlbumArtworkURL: "album-art", AlbumArtistArtworkURL: "artist-art",
	}
	noArtistArt := song
	noArtistArt.AlbumArtistArtworkURL = ""
	tests := []struct {
		name     string
		format   string
		song     applemusic.Song
		typ      applemusic.URLType
		existing map[string]string
		replace  bool
		// playlist, when set, saves that playlist folder's cover instead.
		playlist string
		want     map[string]string
	}{
		{name: "album add", format: "jpg", song: song, typ: applemusic.TypeAlbum, want: map[string]string{
			"library/Artist/Album/cover.jpg": "album-art jpg", "library/Artist/artist.jpg": "artist-art jpg",
		}},
		{name: "png format", format: "png", song: song, typ: applemusic.TypeSong, want: map[string]string{
			"library/Artist/Album/cover.png": "album-art png", "library/Artist/artist.png": "artist-art png",
		}},
		{name: "artist art read from the catalog", format: "jpg", song: noArtistArt, typ: applemusic.TypeArtist, want: map[string]string{
			"library/Artist/Album/cover.jpg": "album-art jpg", "library/Artist/artist.jpg": "catalog-artist-art jpg",
		}},
		{name: "playlist track writes no album covers", format: "jpg", song: song, typ: applemusic.TypePlaylist, want: map[string]string{}},
		{name: "station track writes no album covers", format: "jpg", song: song, typ: applemusic.TypeStation, want: map[string]string{}},
		{name: "existing cover kept", format: "jpg", song: song, typ: applemusic.TypeAlbum,
			existing: map[string]string{"library/Artist/Album/cover.jpg": "mine"},
			want:     map[string]string{"library/Artist/Album/cover.jpg": "mine", "library/Artist/artist.jpg": "artist-art jpg"}},
		{name: "playlist cover", format: "jpg", playlist: "playlists/Mix", want: map[string]string{
			"playlists/Mix/cover.jpg": "playlist-art jpg",
		}},
		{name: "playlist cover reset replaces", format: "png", playlist: "playlists/Mix", replace: true,
			existing: map[string]string{"playlists/Mix/cover.png": "old"},
			want:     map[string]string{"playlists/Mix/cover.png": "playlist-art png"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Download.DownloadsDir = t.TempDir()
			cfg.Download.CoverFormat = tt.format
			cfg.Download.MaxAttempts = 1
			for rel, body := range tt.existing {
				writeTestFile(t, filepath.Join(cfg.Download.DownloadsDir, rel), body)
			}
			catalog := fakeDownloaderCatalog{
				artist: applemusic.Artist{ID: "art-1", ArtworkURL: "catalog-artist-art"},
				cover: func(_ context.Context, url, format, _ string) ([]byte, error) {
					return []byte(url + " " + format), nil
				},
			}
			d := newTestDownloader(&Downloader{cfg: cfg, catalog: catalog})
			var err error
			if tt.playlist != "" {
				err = d.savePlaylistCover(t.Context(), "playlist-art", filepath.Join(cfg.Download.DownloadsDir, tt.playlist), tt.replace)
			} else {
				albumDir, artistDir := libraryCoverDirs(cfg, album)
				err = d.saveStandaloneCovers(t.Context(), tt.song, tt.typ, "cn", albumDir, artistDir)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := treeContents(t, cfg.Download.DownloadsDir); !maps.Equal(got, tt.want) {
				t.Fatalf("files = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStandaloneCoverRejectsAnUnknownFormat(t *testing.T) {
	cfg := config.Default()
	cfg.Download.CoverFormat = "webp"
	d := newTestDownloader(&Downloader{cfg: cfg})
	if err := d.savePlaylistCover(t.Context(), "art", t.TempDir(), false); err == nil {
		t.Fatal("savePlaylistCover with cover_format webp succeeded")
	}
}

// writeTestFile writes body at path, creating its folders.
func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// treeContents maps each regular file under root, by its slash-separated
// path relative to root, to its contents.
func treeContents(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
