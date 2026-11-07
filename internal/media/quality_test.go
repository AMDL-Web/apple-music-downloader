package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/jobs"
	"amdl/internal/limits"
	"amdl/internal/usertoken"
	"amdl/internal/wrapper"
)

const qualityTestMediaPlaylist = `#EXTM3U
#EXT-X-MAP:URI="media.mp4"
`

func writeQualityTestPlaylist(w http.ResponseWriter, r *http.Request, master string) {
	if r.URL.Path == "/audio.m3u8" {
		_, _ = w.Write([]byte(qualityTestMediaPlaylist))
		return
	}
	_, _ = w.Write([]byte(master))
}

func TestQualityQueryUsesSharedVariantSelectionWithoutMediaPlaylist(t *testing.T) {
	const master = `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-stereo-256",NAME="AAC",URI="aac/prog.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=281000,AVERAGE-BANDWIDTH=256000,CODECS="mp4a.40.2",AUDIO="audio-stereo-256"
aac/media.m3u8
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-alac-stereo-96000-24",NAME="ALAC",URI="alac/prog.m3u8",BIT-DEPTH=24,SAMPLE-RATE=96000
#EXT-X-STREAM-INF:BANDWIDTH=4600000,AVERAGE-BANDWIDTH=3900000,CODECS="alac",AUDIO="audio-alac-stereo-96000-24"
alac/media.m3u8
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-alac-stereo-192000-24",NAME="ALAC Hi-Res",URI="alac-hires/prog.m3u8",BIT-DEPTH=24,SAMPLE-RATE=192000
#EXT-X-STREAM-INF:BANDWIDTH=9200000,AVERAGE-BANDWIDTH=7800000,CODECS="alac",AUDIO="audio-alac-stereo-192000-24"
alac-hires/media.m3u8
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-atmos-2448",NAME="Atmos",URI="atmos/prog.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=768000,AVERAGE-BANDWIDTH=640000,CODECS="ec-3",AUDIO="audio-atmos-2448"
atmos/media.m3u8
`
	var masterHits, mediaHits int
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			masterHits++
			_, _ = w.Write([]byte(master))
			return
		}
		mediaHits++
		_, _ = w.Write([]byte(qualityTestMediaPlaylist))
	}))
	defer manifest.Close()

	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	cfg.Download.ALACMaxSampleRate = 96000
	service := newTestQualityService(cfg, fakeDownloaderCatalog{song: applemusic.Song{
		ID: "song-1", Name: "Song", ArtistName: "Artist", AlbumName: "Album", EnhancedHLS: manifest.URL + "/master.m3u8",
	}}, fakeDownloaderWrapper{status: readyWrapper})
	result, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/song/example/song-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tracks) != 1 || result.Tracks[0].Song.ID != "song-1" {
		t.Fatalf("song result tracks = %#v, want one song-1 track", result.Tracks)
	}
	track := result.Tracks[0]

	want := map[string]QualityOption{
		"aac":  {Available: true, CodecID: "audio-stereo-256", Bitrate: 256000},
		"alac": {Available: true, CodecID: "audio-alac-stereo-96000-24", Bitrate: 3900000, BitDepth: 24, SampleRate: 96000},
		"ec3":  {Available: true, CodecID: "audio-atmos-2448", Bitrate: 640000},
	}
	got := map[string]QualityOption{}
	wantOrder := []string{"aac", "aac-binaural", "aac-downmix", "alac", "ec3", "ac3"}
	for i, quality := range track.Qualities {
		if quality.ID != wantOrder[i] {
			t.Fatalf("quality order[%d] = %q, want %q", i, quality.ID, wantOrder[i])
		}
		got[quality.ID] = quality
	}
	for id, expected := range want {
		actual, ok := got[id]
		if !ok {
			t.Fatalf("quality %q missing from %#v", id, track.Qualities)
		}
		if !actual.Available || actual.CodecID != expected.CodecID || actual.Bitrate != expected.Bitrate || actual.BitDepth != expected.BitDepth || actual.SampleRate != expected.SampleRate {
			t.Fatalf("quality %q = %#v, want fields %#v", id, actual, expected)
		}
	}
	if got["alac"].Description == "" {
		t.Fatalf("ALAC description is empty")
	}
	if masterHits != 1 || mediaHits != 0 {
		t.Fatalf("playlist requests = master:%d media:%d, want master:1 media:0", masterHits, mediaHits)
	}

	selected, err := service.downloader.selectEnhancedStream(context.Background(), "cn", applemusic.Song{
		ID: "song-1", EnhancedHLS: manifest.URL + "/master.m3u8",
	}, "alac")
	if err != nil {
		t.Fatal(err)
	}
	if selected.CodecID != got["alac"].CodecID || selected.BitDepth != got["alac"].BitDepth || selected.SampleRate != got["alac"].SampleRate || selected.Bandwidth != got["alac"].Bitrate {
		t.Fatalf("download selection = %+v, quality variant = %+v", selected, got["alac"])
	}
	if masterHits != 2 || mediaHits != 1 {
		t.Fatalf("combined playlist requests = master:%d media:%d, want master:2 media:1", masterHits, mediaHits)
	}
}

func TestQualityQueryUsesDownloadStorefrontValidation(t *testing.T) {
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	catalog := fakeDownloaderCatalog{song: applemusic.Song{ID: "song-1", Name: "Song"}}

	tests := []struct {
		name     string
		wrapper  fakeDownloaderWrapper
		wantCode string
	}{
		{name: "decryptor unavailable", wrapper: fakeDownloaderWrapper{statusErr: errors.New("offline")}, wantCode: "decryptor_unavailable"},
		// The account storefront (here catalog.default_storefront, with no
		// token and two wrapper regions) is the one checked, not the link's.
		{name: "account storefront not served", wrapper: fakeDownloaderWrapper{status: wrapper.Status{Ready: true, LoggedIn: true, Regions: []string{"us", "jp"}}}, wantCode: "decryptor_unavailable"},
	}
	cfg.Catalog.DefaultStorefront = "cn"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestQualityService(cfg, catalog, tt.wrapper)
			_, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/song/example/song-1"})
			var requestErr *jobs.RequestError
			if !errors.As(err, &requestErr) || requestErr.Code != tt.wantCode {
				t.Fatalf("error = %v, want request error code %q", err, tt.wantCode)
			}
		})
	}
}

func TestQualityQueryUsesCatalogManifestWithoutWrapperM3U8(t *testing.T) {
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		writeQualityTestPlaylist(w, r, `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-stereo-256",NAME="AAC"
#EXT-X-STREAM-INF:BANDWIDTH=281000,AVERAGE-BANDWIDTH=256000,CODECS="mp4a.40.2",AUDIO="audio-stereo-256"
audio.m3u8
`)
	}))
	defer manifest.Close()

	service := newTestQualityService(config.Default(), fakeDownloaderCatalog{song: applemusic.Song{
		ID: "song-1", Name: "Song", ArtistName: "Artist", AlbumName: "Album", HasLyrics: true, EnhancedHLS: manifest.URL + "/master.m3u8",
	}}, fakeDownloaderWrapper{status: readyWrapper})

	result, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/song/example/song-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tracks) != 1 || len(result.Tracks[0].Qualities) == 0 || !result.Tracks[0].Qualities[0].Available {
		t.Fatalf("expected catalog manifest qualities, got %#v", result.Tracks)
	}
	if !result.Tracks[0].Song.HasLyrics {
		t.Fatalf("song = %+v, want has_lyrics propagated from catalog", result.Tracks[0].Song)
	}
}

func TestQualityQuerySongWithoutEnhancedHLSOffersNothing(t *testing.T) {
	service := newTestQualityService(config.Default(), fakeDownloaderCatalog{song: applemusic.Song{
		ID: "song-1", Name: "Song", ArtistName: "Artist", AlbumName: "Album",
	}}, fakeDownloaderWrapper{status: readyWrapper})

	result, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/song/example/song-1"})
	if err != nil {
		t.Fatalf("QueryQuality = %v, want every codec reported as not offered", err)
	}
	if len(result.Tracks) != 1 || len(result.Tracks[0].Qualities) != 6 {
		t.Fatalf("tracks = %#v, want one track with all six codecs", result.Tracks)
	}
	for _, q := range result.Tracks[0].Qualities {
		if q.Available {
			t.Fatalf("quality %s available without an Enhanced HLS master", q.ID)
		}
	}
}

func TestQualityMasterInventoryRetriesSourceAndMasterAsOneUnit(t *testing.T) {
	var masterHits atomic.Int32
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if masterHits.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-stereo-256",NAME="AAC"
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=256000,AUDIO="audio-stereo-256"
audio.m3u8
`))
	}))
	defer manifest.Close()

	// The server runs outside the bubble; the retry backoff runs on its clock.
	synctest.Test(t, func(t *testing.T) {
		cfg := config.Default()
		cfg.Download.MaxAttempts = 2
		calls := &callLog{}
		d := newTestDownloader(&Downloader{cfg: cfg, http: bubbleClient(), catalog: fakeDownloaderCatalog{song: applemusic.Song{
			ID: "song-1", Name: "Song", ArtistName: "Artist", AlbumName: "Album", HiResLossless: true,
		}}, wrapper: fakeDownloaderWrapper{status: readyWrapper, m3u8: manifest.URL + "/master.m3u8", calls: calls}})
		service := NewQualityService(d)

		result, err := service.QueryQuality(t.Context(), QualityRequest{URL: "https://music.apple.com/cn/song/example/song-1"})
		if err != nil {
			t.Fatal(err)
		}
		if wrapperCalls := len(calls.with("wrapper m3u8 ")); wrapperCalls != 2 || masterHits.Load() != 2 {
			t.Fatalf("retry calls = wrapper:%d master:%d, want wrapper:2 master:2", wrapperCalls, masterHits.Load())
		}
		if len(result.Tracks) != 1 || len(result.Tracks[0].Qualities) == 0 || !result.Tracks[0].Qualities[0].Available {
			t.Fatalf("qualities after retry = %#v", result.Tracks)
		}
	})
}

// hiResManifests serves the two masters of a hi-res-lossless track the
// way Apple did for song 1635388718: the web player's enhancedHls offers ALAC
// only at 48 kHz, while the device manifest also lists 88.2 kHz.
func hiResManifests(t *testing.T) (webMaster, deviceMaster string) {
	t.Helper()
	const alac48 = `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-alac-stereo-48000-24",NAME="ALAC",BIT-DEPTH=24,SAMPLE-RATE=48000
#EXT-X-STREAM-INF:BANDWIDTH=2400000,AVERAGE-BANDWIDTH=2000000,CODECS="alac",AUDIO="audio-alac-stereo-48000-24"
alac-48/media.m3u8
`
	const alac88 = `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-alac-stereo-88200-24",NAME="ALAC",BIT-DEPTH=24,SAMPLE-RATE=88200
#EXT-X-STREAM-INF:BANDWIDTH=4300000,AVERAGE-BANDWIDTH=3600000,CODECS="alac",AUDIO="audio-alac-stereo-88200-24"
alac-88/media.m3u8
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/web.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" + alac48))
		case "/device.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" + alac48 + alac88))
		case "/alac-48/media.m3u8", "/alac-88/media.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://itunes.apple.com/P000000000/s1/e1/c23\"\n" +
				"#EXT-X-MAP:URI=\"encrypted.mp4\"\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL + "/web.m3u8", server.URL + "/device.m3u8"
}

func TestQualityQueryHiResTrackReadsDeviceManifest(t *testing.T) {
	webMaster, deviceMaster := hiResManifests(t)
	tests := []struct {
		name          string
		hiRes         bool
		maxRate       int // alac_max_sample_rate; 0 keeps the default
		wrapper       fakeDownloaderWrapper
		wantRate      int
		wantM3U8Calls int
	}{
		{name: "hi-res track takes the device manifest", hiRes: true, wrapper: fakeDownloaderWrapper{status: readyWrapper, m3u8: deviceMaster}, wantRate: 88200, wantM3U8Calls: 1},
		{name: "wrapper error keeps the web manifest", hiRes: true, wrapper: fakeDownloaderWrapper{status: readyWrapper, m3u8Err: errors.New("wrapper m3u8: no session")}, wantRate: 48000, wantM3U8Calls: 1},
		{name: "empty device manifest keeps the web manifest", hiRes: true, wrapper: fakeDownloaderWrapper{status: readyWrapper}, wantRate: 48000, wantM3U8Calls: 1},
		{name: "other tracks never ask the wrapper", hiRes: false, wrapper: fakeDownloaderWrapper{status: readyWrapper, m3u8: deviceMaster}, wantRate: 48000, wantM3U8Calls: 0},
		{name: "a 48 kHz cap never asks the wrapper", hiRes: true, maxRate: 48000, wrapper: fakeDownloaderWrapper{status: readyWrapper, m3u8: deviceMaster}, wantRate: 48000, wantM3U8Calls: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Download.MaxAttempts = 1
			if tt.maxRate != 0 {
				cfg.Download.ALACMaxSampleRate = tt.maxRate
			}
			log := &callLog{}
			tt.wrapper.calls = log
			service := newTestQualityService(cfg, fakeDownloaderCatalog{song: applemusic.Song{
				ID: "song-1", Name: "TV", EnhancedHLS: webMaster, HiResLossless: tt.hiRes,
			}}, tt.wrapper)

			result, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/song/example/song-1"})
			if err != nil {
				t.Fatal(err)
			}
			if calls := len(log.with("wrapper m3u8 ")); calls != tt.wantM3U8Calls {
				t.Fatalf("wrapper.M3U8 calls = %d, want %d", calls, tt.wantM3U8Calls)
			}
			var alac QualityOption
			for _, option := range result.Tracks[0].Qualities {
				if option.ID == "alac" {
					alac = option
				}
			}
			if !alac.Available || alac.SampleRate != tt.wantRate {
				t.Fatalf("alac = %+v, want sample rate %d", alac, tt.wantRate)
			}
		})
	}
}

func TestSelectEnhancedStreamHiResDownloadsFromDeviceManifest(t *testing.T) {
	webMaster, deviceMaster := hiResManifests(t)
	log := &callLog{}
	d := newTestDownloader(&Downloader{
		cfg: config.Default(), wrapper: fakeDownloaderWrapper{status: readyWrapper, m3u8: deviceMaster, calls: log},
	})
	info, err := d.selectEnhancedStream(context.Background(), "cn", applemusic.Song{ID: "song-1", EnhancedHLS: webMaster, HiResLossless: true}, "alac")
	if err != nil {
		t.Fatal(err)
	}
	if calls := len(log.with("wrapper m3u8 ")); calls != 1 || info.SampleRate != 88200 || info.CodecID != "audio-alac-stereo-88200-24" || !strings.HasSuffix(info.MediaURI, "/alac-88/encrypted.mp4") {
		t.Fatalf("selected %+v after %v, want the 88.2 kHz device rendition", info, log.list())
	}
}

func TestResolveEnhancedHLSHiResDoesNotFallBackWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := newTestDownloader(&Downloader{cfg: config.Default(), wrapper: fakeDownloaderWrapper{status: readyWrapper, m3u8Err: ctx.Err()}})
	master, _, err := d.resolveEnhancedHLS(ctx, "cn", applemusic.Song{ID: "song-1", EnhancedHLS: "https://web.invalid/master.m3u8", HiResLossless: true}, "alac")
	if !errors.Is(err, context.Canceled) || master != "" {
		t.Fatalf("resolveEnhancedHLS = %q, %v; want context.Canceled, not the web manifest", master, err)
	}
}

var userToken = strings.Repeat("u", 32)

const qualityTestMaster = `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-stereo-256",NAME="AAC"
#EXT-X-STREAM-INF:BANDWIDTH=281000,AVERAGE-BANDWIDTH=256000,CODECS="mp4a.40.2",AUDIO="audio-stereo-256"
audio.m3u8
`

// newCollectionQualityCatalog is two albums, a playlist, a station and an
// artist over three songs whose masters manifestBase serves; calls records
// the catalog reads.
func newCollectionQualityCatalog(manifestBase string, calls *callLog) fakeDownloaderCatalog {
	return fakeDownloaderCatalog{
		calls: calls,
		songs: map[string]applemusic.Song{
			"s1": {ID: "s1", Name: "One", ArtistName: "Artist", AlbumName: "First", HasLyrics: true, EnhancedHLS: manifestBase + "/s1.m3u8"},
			"s2": {ID: "s2", Name: "Two", ArtistName: "Artist", AlbumName: "First", EnhancedHLS: manifestBase + "/s2.m3u8"},
			"s3": {ID: "s3", Name: "Three", ArtistName: "Artist", AlbumName: "Second", EnhancedHLS: manifestBase + "/s3.m3u8"},
		},
		albums: map[string]applemusic.Collection{
			"a1": {ID: "a1", Tracks: []applemusic.Song{{ID: "s1", AlbumName: "First"}, {ID: "s2", AlbumName: "First"}}},
			"a2": {ID: "a2", Tracks: []applemusic.Song{{ID: "s2", AlbumName: "Second"}, {ID: "s3", AlbumName: "Second"}}},
		},
		playlist: applemusic.Collection{ID: "pl.1", Tracks: []applemusic.Song{{ID: "s2"}, {ID: "s1"}}},
		station:  applemusic.Collection{ID: "ra.1", Tracks: []applemusic.Song{{ID: "s1"}, {ID: "s3"}}},
		artistAlbums: applemusic.ArtistAlbums{Artist: applemusic.Artist{ID: "ar.1"}, Albums: []applemusic.Collection{
			{ID: "a1"}, {ID: "a2"},
		}},
		songErrs: map[string]error{},
	}
}

func TestQualityQuerySupportsCollectionURLTypes(t *testing.T) {
	var manifestMu sync.Mutex
	manifestHits := map[string]int{}
	mediaHits := 0
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio.m3u8" {
			manifestMu.Lock()
			mediaHits++
			manifestMu.Unlock()
			writeQualityTestPlaylist(w, r, qualityTestMaster)
			return
		}
		manifestMu.Lock()
		manifestHits[r.URL.Path]++
		manifestMu.Unlock()
		writeQualityTestPlaylist(w, r, qualityTestMaster)
	}))
	defer manifest.Close()

	tests := []struct {
		name      string
		url       string
		wantType  applemusic.URLType
		wantID    string
		wantSongs []string
		check     func(*testing.T, *callLog)
	}{
		{name: "album", url: "https://music.apple.com/cn/album/example/a1", wantType: applemusic.TypeAlbum, wantID: "a1", wantSongs: []string{"s1", "s2"}, check: func(t *testing.T, calls *callLog) {
			if got := calls.with("album "); !reflect.DeepEqual(got, []string{"cn/a1"}) {
				t.Fatalf("album calls = %#v", got)
			}
		}},
		{name: "playlist", url: "https://beta.music.apple.com/cn/playlist/example/pl.1", wantType: applemusic.TypePlaylist, wantID: "pl.1", wantSongs: []string{"s2", "s1"}, check: func(t *testing.T, calls *callLog) {
			if got := calls.with("token "); !reflect.DeepEqual(got, []string{userToken}) {
				t.Fatalf("playlist tokens = %#v", got)
			}
		}},
		{name: "station", url: "https://music.apple.com/cn/station/example/ra.1", wantType: applemusic.TypeStation, wantID: "ra.1", wantSongs: []string{"s1", "s3"}, check: func(t *testing.T, calls *callLog) {
			if got := calls.with("token "); !reflect.DeepEqual(got, []string{userToken}) {
				t.Fatalf("station tokens = %#v", got)
			}
		}},
		{name: "artist", url: "https://classical.music.apple.com/cn/artist/example/ar.1", wantType: applemusic.TypeArtist, wantID: "ar.1", wantSongs: []string{"s1", "s2", "s2", "s3"}, check: func(t *testing.T, calls *callLog) {
			if artists, albums := calls.with("artist "), calls.with("album "); !reflect.DeepEqual(artists, []string{"cn/ar.1"}) || !reflect.DeepEqual(albums, []string{"cn/a1", "cn/a2"}) {
				t.Fatalf("artist/album calls = %#v/%#v", artists, albums)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := &callLog{}
			catalog := newCollectionQualityCatalog(manifest.URL, calls)
			service := NewQualityService(newTestDownloader(&Downloader{
				cfg:     config.Default(),
				tokens:  usertoken.NewReading(func(string) (string, error) { return "  " + userToken + "  ", nil }),
				catalog: catalog, wrapper: fakeDownloaderWrapper{status: readyWrapper},
			}))

			result, err := service.QueryQuality(context.Background(), QualityRequest{URL: tt.url})
			if err != nil {
				t.Fatal(err)
			}
			if result.Type != string(tt.wantType) || result.AdamID != tt.wantID {
				t.Fatalf("result header = %#v", result)
			}
			gotSongs := make([]string, len(result.Tracks))
			for i, track := range result.Tracks {
				gotSongs[i] = track.Song.ID
				if len(track.Qualities) != len(qualitySpecs) || !track.Qualities[0].Available {
					t.Fatalf("track %d qualities = %#v", i, track.Qualities)
				}
			}
			if !reflect.DeepEqual(gotSongs, tt.wantSongs) {
				t.Fatalf("track order = %#v, want %#v", gotSongs, tt.wantSongs)
			}
			if tt.name == "artist" && result.Tracks[2].Song.Album != "Second" {
				t.Fatalf("duplicate song lost occurrence album: %#v", result.Tracks[2].Song)
			}
			if got, want := len(calls.with("song ")), len(tt.wantSongs); got != want {
				t.Fatalf("SongMetadata calls = %d, want %d track occurrences", got, want)
			}
			tt.check(t, calls)
		})
	}

	manifestMu.Lock()
	defer manifestMu.Unlock()
	if manifestHits["/s2.m3u8"] != 4 {
		t.Fatalf("s2 manifest hits = %d, want one inventory for each track occurrence", manifestHits["/s2.m3u8"])
	}
	if mediaHits != 0 {
		t.Fatalf("media playlist hits = %d, want quality inventory to stop at the master playlist", mediaHits)
	}
}

func TestQualityQueryAlbumTrackModeUsesUnifiedTracks(t *testing.T) {
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeQualityTestPlaylist(w, r, qualityTestMaster)
	}))
	defer manifest.Close()

	calls := &callLog{}
	catalog := newCollectionQualityCatalog(manifest.URL, calls)
	cfg := config.Default()
	cfg.Catalog.AlbumTrackURLMode = "song"
	service := newTestQualityService(cfg, catalog, fakeDownloaderWrapper{status: readyWrapper})
	result, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/album/example/a1?i=s1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "song" || result.AdamID != "s1" || len(result.Tracks) != 1 || result.Tracks[0].Song.ID != "s1" {
		t.Fatalf("song-mode result = %#v", result)
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"tracks"`) || !strings.Contains(string(body), `"song"`) || !strings.Contains(string(body), `"qualities"`) {
		t.Fatalf("unified song wire shape = %s", body)
	}

	cfg.Catalog.AlbumTrackURLMode = "album"
	service = newTestQualityService(cfg, catalog, fakeDownloaderWrapper{status: readyWrapper})
	result, err = service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/album/example/a1?i=s1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "album" || len(result.Tracks) != 2 {
		t.Fatalf("album-mode result = %#v", result)
	}
}

func TestQualityQueryReadsTheTokenOnEveryQuery(t *testing.T) {
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeQualityTestPlaylist(w, r, qualityTestMaster)
	}))
	defer manifest.Close()
	calls := &callLog{}
	catalog := newCollectionQualityCatalog(manifest.URL, calls)
	catalog.station.Tracks[0].EnhancedHLS = manifest.URL + "/s1.m3u8"
	catalog.station.Tracks = catalog.station.Tracks[:1]
	first, second := strings.Repeat("f", 32), strings.Repeat("s", 32)
	var current atomic.Value
	current.Store(first)
	cfg := config.Default()
	service := NewQualityService(newTestDownloader(&Downloader{
		cfg:     cfg,
		tokens:  usertoken.NewReading(func(string) (string, error) { return current.Load().(string), nil }),
		catalog: catalog, wrapper: fakeDownloaderWrapper{status: readyWrapper},
	}))
	url := "https://music.apple.com/cn/station/example/ra.1"
	if _, err := service.QueryQuality(context.Background(), QualityRequest{URL: url}); err != nil {
		t.Fatal(err)
	}
	current.Store(" " + second + " ")
	if _, err := service.QueryQuality(context.Background(), QualityRequest{URL: url}); err != nil {
		t.Fatal(err)
	}
	if got := calls.with("token "); !reflect.DeepEqual(got, []string{first, second}) {
		t.Fatalf("station tokens = %#v", got)
	}
}

func TestQualityQueryCollectionAsksTheWrapperOnlyForHiResTracks(t *testing.T) {
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeQualityTestPlaylist(w, r, qualityTestMaster)
	}))
	defer manifest.Close()

	calls := &callLog{}
	catalog := newCollectionQualityCatalog(manifest.URL, calls)
	hiRes := catalog.songs["s1"]
	hiRes.HiResLossless = true
	catalog.songs["s1"] = hiRes
	wrapperCalls := &callLog{}
	wrapper := fakeDownloaderWrapper{status: readyWrapper, m3u8For: map[string]string{"s1": manifest.URL + "/s1-device.m3u8"}, calls: wrapperCalls}
	service := newTestQualityService(config.Default(), catalog, wrapper)
	result, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/album/example/a1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tracks) != 2 {
		t.Fatalf("tracks = %#v", result.Tracks)
	}
	if got := wrapperCalls.with("wrapper m3u8 "); !slices.Equal(got, []string{"s1"}) {
		t.Fatalf("wrapper.M3U8 calls = %#v, want only the hi-res track s1", got)
	}
}

func TestQualityQueryRejectsUnsupportedAndEmptyCollections(t *testing.T) {
	catalog := newCollectionQualityCatalog("https://example.invalid", nil)
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	service := newTestQualityService(cfg, catalog, fakeDownloaderWrapper{status: readyWrapper})
	if _, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/music-video/example/mv.1"}); err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("music-video error = %v", err)
	}
	catalog.albums["empty"] = applemusic.Collection{ID: "empty"}
	if _, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/album/example/empty"}); err == nil || !strings.Contains(err.Error(), "no downloadable songs") {
		t.Fatalf("empty album error = %v", err)
	}
	catalog.songErrs["s1"] = errors.New("metadata failed")
	if _, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/album/example/a1"}); err == nil || !strings.Contains(err.Error(), "metadata failed") {
		t.Fatalf("track error = %v", err)
	}
}

func TestQualityQueryUsesSharedRequestGateAndPreservesOrder(t *testing.T) {
	const maxParallelRequests = 3
	started := make(chan string, 8)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()
	var startedMu sync.Mutex
	startedPaths := map[string]bool{}
	mediaHits := 0
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio.m3u8" {
			startedMu.Lock()
			mediaHits++
			startedMu.Unlock()
			writeQualityTestPlaylist(w, r, qualityTestMaster)
			return
		}
		startedMu.Lock()
		first := !startedPaths[r.URL.Path]
		startedPaths[r.URL.Path] = true
		startedMu.Unlock()
		if first {
			started <- strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".m3u8")
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		writeQualityTestPlaylist(w, r, qualityTestMaster)
	}))
	defer manifest.Close()

	calls := &callLog{}
	catalog := newCollectionQualityCatalog(manifest.URL, calls)
	tracks := make([]applemusic.Song, 8)
	for i := range tracks {
		id := "s" + string(rune('a'+i))
		tracks[i] = applemusic.Song{ID: id, Name: id}
		catalog.songs[id] = applemusic.Song{ID: id, Name: id, EnhancedHLS: manifest.URL + "/" + id + ".m3u8"}
	}
	catalog.albums["many"] = applemusic.Collection{ID: "many", Tracks: tracks}
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	service := newTestQualityService(cfg, catalog, fakeDownloaderWrapper{status: readyWrapper})
	service.downloader.requestGate = limits.NewRequestGate(maxParallelRequests, 1000, len(tracks))
	type queryResult struct {
		result QualityResult
		err    error
	}
	done := make(chan queryResult, 1)
	go func() {
		result, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/album/example/many"})
		done <- queryResult{result: result, err: err}
	}()
	for range maxParallelRequests {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for shared request-gate slots")
		}
	}
	select {
	case id := <-started:
		t.Fatalf("more than %d manifest requests started before release: %s", maxParallelRequests, id)
	case <-time.After(100 * time.Millisecond):
	}
	releaseAll()
	query := <-done
	if query.err != nil {
		t.Fatal(query.err)
	}
	for i, track := range query.result.Tracks {
		if track.Song.ID != tracks[i].ID {
			t.Fatalf("track %d = %s, want %s", i, track.Song.ID, tracks[i].ID)
		}
	}
	startedMu.Lock()
	defer startedMu.Unlock()
	if len(startedPaths) != len(tracks) {
		t.Fatalf("master playlist paths = %#v, want one per track", startedPaths)
	}
	if mediaHits != 0 {
		t.Fatalf("media playlist hits = %d, want 0", mediaHits)
	}
}

// One failed probe fails the whole response, so it cancels the probes still
// running: the query returns that failure while a sibling's manifest request
// is still hanging, instead of waiting for it.
func TestQualityQueryCancelsSiblingProbesAfterFirstFailure(t *testing.T) {
	blockedStarted := make(chan struct{})
	blockedCancelled := make(chan struct{})
	releaseBlocked := make(chan struct{})
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/failed.m3u8":
			select {
			case <-blockedStarted:
			case <-r.Context().Done():
				return
			}
			http.Error(w, "upstream failed", http.StatusBadGateway)
		case "/blocked.m3u8":
			select {
			case <-blockedStarted:
			default:
				close(blockedStarted)
			}
			select {
			case <-r.Context().Done():
				close(blockedCancelled)
			case <-releaseBlocked:
				writeQualityTestPlaylist(w, r, qualityTestMaster)
			}
		default:
			writeQualityTestPlaylist(w, r, qualityTestMaster)
		}
	}))
	defer manifest.Close()
	defer close(releaseBlocked)

	calls := &callLog{}
	catalog := newCollectionQualityCatalog(manifest.URL, calls)
	catalog.albums["cancel"] = applemusic.Collection{ID: "cancel", Tracks: []applemusic.Song{{ID: "failed"}, {ID: "blocked"}}}
	catalog.songs["failed"] = applemusic.Song{ID: "failed", EnhancedHLS: manifest.URL + "/failed.m3u8"}
	catalog.songs["blocked"] = applemusic.Song{ID: "blocked", EnhancedHLS: manifest.URL + "/blocked.m3u8"}
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	service := newTestQualityService(cfg, catalog, fakeDownloaderWrapper{status: readyWrapper})
	done := make(chan error, 1)
	go func() {
		_, err := service.QueryQuality(context.Background(), QualityRequest{URL: "https://music.apple.com/cn/album/example/cancel"})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "502 Bad Gateway") {
			t.Fatalf("query error = %v, want the failed probe's 502", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("query waited for the hanging sibling probe after the first failure")
	}
	select {
	case <-blockedCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the sibling probe's request was not cancelled")
	}
}
