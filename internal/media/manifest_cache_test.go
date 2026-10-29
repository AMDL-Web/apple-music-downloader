package media

import (
	"context"
	"strconv"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

func TestDeviceManifestCacheExpiresAfterTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cache := newDeviceManifestCache()
	cache.now = func() time.Time { return now }
	key := deviceManifestKey{storefront: "cn", songID: "1"}

	cache.put(key, "https://cdn.invalid/a.m3u8")
	now = now.Add(deviceManifestTTL - time.Second)
	if url, ok := cache.get(key); !ok || url != "https://cdn.invalid/a.m3u8" {
		t.Fatalf("get just before the TTL = %q, %v; want the stored URL", url, ok)
	}
	now = now.Add(time.Second)
	if url, ok := cache.get(key); ok {
		t.Fatalf("get at the TTL = %q, true; want a miss", url)
	}
}

func TestDeviceManifestCacheKeysByStorefrontAndSong(t *testing.T) {
	cache := newDeviceManifestCache()
	cache.put(deviceManifestKey{storefront: "cn", songID: "1"}, "cn-1")
	cache.put(deviceManifestKey{storefront: "us", songID: "1"}, "us-1")
	if url, _ := cache.get(deviceManifestKey{storefront: "us", songID: "1"}); url != "us-1" {
		t.Fatalf("us/1 = %q, want us-1", url)
	}
	if _, ok := cache.get(deviceManifestKey{storefront: "cn", songID: "2"}); ok {
		t.Fatal("cn/2 hit, want a miss")
	}
	cache.forget(deviceManifestKey{storefront: "cn", songID: "1"})
	if _, ok := cache.get(deviceManifestKey{storefront: "cn", songID: "1"}); ok {
		t.Fatal("cn/1 hit after forget")
	}
	if _, ok := cache.get(deviceManifestKey{storefront: "us", songID: "1"}); !ok {
		t.Fatal("forgetting cn/1 dropped us/1")
	}
}

func TestDeviceManifestCacheIsBounded(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cache := newDeviceManifestCache()
	cache.now = func() time.Time { return now }
	for i := range deviceManifestCacheCap {
		now = now.Add(time.Second)
		cache.put(deviceManifestKey{storefront: "cn", songID: strconv.Itoa(i)}, "url")
	}
	now = now.Add(time.Second)
	cache.put(deviceManifestKey{storefront: "cn", songID: "new"}, "url")

	if len(cache.entries) != deviceManifestCacheCap {
		t.Fatalf("entries = %d, want the cap %d", len(cache.entries), deviceManifestCacheCap)
	}
	if _, ok := cache.get(deviceManifestKey{storefront: "cn", songID: "0"}); ok {
		t.Fatal("the oldest entry survived a full cache")
	}
	if _, ok := cache.get(deviceManifestKey{storefront: "cn", songID: "new"}); !ok {
		t.Fatal("the newest entry is missing")
	}

	// Expired entries go first, so a full cache of stale entries loses all of them.
	now = now.Add(deviceManifestTTL)
	cache.put(deviceManifestKey{storefront: "cn", songID: "fresh"}, "url")
	if len(cache.entries) != 1 {
		t.Fatalf("entries after expiry = %d, want only the fresh one", len(cache.entries))
	}
}

func TestNilDeviceManifestCacheStoresNothing(t *testing.T) {
	var cache *deviceManifestCache
	key := deviceManifestKey{storefront: "cn", songID: "1"}
	cache.put(key, "url")
	cache.forget(key)
	if _, ok := cache.get(key); ok {
		t.Fatal("a nil cache returned a hit")
	}
}

func TestWantsDeviceManifest(t *testing.T) {
	tests := []struct {
		name    string
		hiRes   bool
		codec   string
		maxRate int
		want    bool
	}{
		{name: "hi-res ALAC above 48 kHz", hiRes: true, codec: "alac", maxRate: 192000, want: true},
		{name: "hi-res ALAC capped at 96 kHz", hiRes: true, codec: "alac", maxRate: 96000, want: true},
		{name: "hi-res ALAC capped at 48 kHz", hiRes: true, codec: "alac", maxRate: 48000},
		{name: "hi-res ALAC capped at 44.1 kHz", hiRes: true, codec: "alac", maxRate: 44100},
		{name: "hi-res ALAC without a ceiling (0)", hiRes: true, codec: "alac", maxRate: 0, want: true},
		{name: "hi-res ALAC without a ceiling (negative)", hiRes: true, codec: "alac", maxRate: -1, want: true},
		{name: "ALAC without a ceiling of a track without the trait", codec: "alac", maxRate: 0},
		{name: "ALAC of a track without the trait", codec: "alac", maxRate: 192000},
		{name: "Atmos of a hi-res track", hiRes: true, codec: "ec3", maxRate: 192000},
		{name: "AAC of a hi-res track", hiRes: true, codec: "aac", maxRate: 192000},
		{name: "binaural AAC of a hi-res track", hiRes: true, codec: "aac-binaural", maxRate: 192000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Download.ALACMaxSampleRate = tt.maxRate
			d := &Downloader{cfg: cfg}
			if got := d.wantsDeviceManifest(applemusic.Song{HiResLossless: tt.hiRes}, tt.codec); got != tt.want {
				t.Fatalf("wantsDeviceManifest = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectEnhancedStreamReusesTheDeviceManifest(t *testing.T) {
	webMaster, deviceMaster := hiResManifests(t)
	var calls int
	d := newTestDownloader(&Downloader{
		cfg: config.Default(), wrapper: countingQualityWrapper{m3u8: deviceMaster, callCount: &calls}, http: newHTTPClient(),
	})
	song := applemusic.Song{ID: "song-1", EnhancedHLS: webMaster, HiResLossless: true}

	for range 3 {
		if _, err := d.selectEnhancedStream(context.Background(), "cn", song, "alac"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("wrapper.M3U8 calls after three selections = %d, want 1", calls)
	}
	// A clone bound to a job's overrides shares the cache.
	clone := d.withConfig(d.cfg)
	if _, err := clone.selectEnhancedStream(context.Background(), "cn", song, "alac"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("wrapper.M3U8 calls after a job-scoped clone = %d, want 1", calls)
	}
	if _, err := d.selectEnhancedStream(context.Background(), "us", song, "alac"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("wrapper.M3U8 calls after another storefront = %d, want 2", calls)
	}
}

func TestSelectEnhancedStreamAsksAgainWhenACachedManifestIsGone(t *testing.T) {
	webMaster, deviceMaster := hiResManifests(t)
	var calls int
	d := newTestDownloader(&Downloader{
		cfg: config.Default(), wrapper: countingQualityWrapper{m3u8: deviceMaster, callCount: &calls}, http: newHTTPClient(),
	})
	key := deviceManifestKey{storefront: "cn", songID: "song-1"}
	d.deviceManifests.put(key, deviceMaster+".gone")

	info, err := d.selectEnhancedStream(context.Background(), "cn", applemusic.Song{ID: "song-1", EnhancedHLS: webMaster, HiResLossless: true}, "alac")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || info.SampleRate != 88200 {
		t.Fatalf("selected %+v after %d wrapper.M3U8 calls, want the 88.2 kHz rendition from one fresh request", info, calls)
	}
	if url, _ := d.deviceManifests.get(key); url != deviceMaster {
		t.Fatalf("cached URL = %q, want the wrapper's current answer %q", url, deviceMaster)
	}
}

func TestSelectEnhancedStreamForgetsAManifestThatDoesNotLoad(t *testing.T) {
	webMaster, deviceMaster := hiResManifests(t)
	var calls int
	d := newTestDownloader(&Downloader{
		cfg: config.Default(), wrapper: countingQualityWrapper{m3u8: deviceMaster + ".gone", callCount: &calls}, http: newHTTPClient(),
	})
	song := applemusic.Song{ID: "song-1", EnhancedHLS: webMaster, HiResLossless: true}

	if _, err := d.selectEnhancedStream(context.Background(), "cn", song, "alac"); err == nil {
		t.Fatal("selection from a manifest that does not exist succeeded")
	}
	if _, ok := d.deviceManifests.get(deviceManifestKey{storefront: "cn", songID: "song-1"}); ok {
		t.Fatal("the failed manifest is still cached")
	}
}
