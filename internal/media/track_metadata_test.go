package media

import (
	"context"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

func TestTrackMetadataResolverReusesResolvedSingleSong(t *testing.T) {
	catalog := &metadataCountingCatalog{
		metadataCalls: make(map[string]int),
		albumCalls:    make(map[string]int),
	}
	downloader := &Downloader{cfg: config.Default(), catalog: catalog}
	initial := applemusic.Song{
		ID: "song-1", Name: "Song", AlbumID: "album-1", AlbumArtist: "Album Artist",
		TrackCount: 10, DiscCount: 2, HasLyrics: true,
	}

	got, err := newTrackMetadataResolver(downloader, "cn").song(context.Background(), initial, applemusic.TypeSong)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != initial.ID || got.Name != initial.Name || got.AlbumID != initial.AlbumID || got.TrackCount != initial.TrackCount || got.DiscCount != initial.DiscCount || got.HasLyrics != initial.HasLyrics {
		t.Fatalf("song = %+v, want resolved value %+v", got, initial)
	}
	_, metadataCalls, albumCalls := catalog.counts()
	if len(metadataCalls) != 0 || len(albumCalls) != 0 {
		t.Fatalf("metadata calls = %v, album calls = %v, want no duplicate reads", metadataCalls, albumCalls)
	}
}

func TestTrackMetadataResolverMergesResolvedAlbumWithoutRefetch(t *testing.T) {
	catalog := &metadataCountingCatalog{
		metadataSongs: map[string]applemusic.Song{
			"song-1": {ID: "song-1", Name: "Fresh Song", AlbumID: "album-1", HasLyrics: false},
		},
		metadataCalls: make(map[string]int),
		albumCalls:    make(map[string]int),
	}
	downloader := &Downloader{cfg: config.Default(), catalog: catalog}
	initial := applemusic.Song{
		ID: "song-1", Name: "Old Song", AlbumID: "album-1", AlbumName: "Album",
		AlbumArtist: "Album Artist", AlbumArtistID: "artist-1", AlbumArtworkURL: "album-art",
		AlbumArtistArtworkURL: "artist-art", AlbumRelease: "2026-01-01", Copyright: "Copyright",
		RecordLabel: "Label", UPC: "123456", TrackCount: 10, DiscCount: 2, HasLyrics: true,
	}

	got, err := newTrackMetadataResolver(downloader, "cn").song(context.Background(), initial, applemusic.TypeAlbum)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Fresh Song" || got.HasLyrics {
		t.Fatalf("refreshed fields = %q/hasLyrics=%v, want Fresh Song/false", got.Name, got.HasLyrics)
	}
	if got.AlbumArtist != "Album Artist" || got.AlbumArtistID != "artist-1" || got.AlbumArtworkURL != "album-art" || got.TrackCount != 10 || got.DiscCount != 2 || got.UPC != "123456" {
		t.Fatalf("album fields were not preserved: %+v", got)
	}
	_, metadataCalls, albumCalls := catalog.counts()
	if metadataCalls["song-1"] != 1 || len(albumCalls) != 0 {
		t.Fatalf("metadata calls = %v, album calls = %v, want one lightweight song read and no album read", metadataCalls, albumCalls)
	}
}

func TestTrackMetadataResolverPreservesArtistAlbumPlacement(t *testing.T) {
	catalog := &metadataCountingCatalog{
		metadataSongs: map[string]applemusic.Song{
			"song-1": {
				ID: "song-1", Name: "Fresh Song", AlbumID: "default-album", AlbumName: "Default Album",
				ArtworkURL: "default-art", TrackNumber: 1, DiscNumber: 1,
			},
		},
		metadataCalls: make(map[string]int),
		albumCalls:    make(map[string]int),
	}
	downloader := &Downloader{cfg: config.Default(), catalog: catalog}
	initial := applemusic.Song{
		ID: "song-1", Name: "Song", AlbumID: "second-album", AlbumName: "Second Album",
		ArtworkURL: "second-art", TrackNumber: 7, DiscNumber: 2,
	}

	got, err := newTrackMetadataResolver(downloader, "cn").song(context.Background(), initial, applemusic.TypeArtist)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Fresh Song" {
		t.Fatalf("song name = %q, want refreshed name Fresh Song", got.Name)
	}
	if got.AlbumID != "second-album" || got.AlbumName != "Second Album" || got.ArtworkURL != "second-art" || got.TrackNumber != 7 || got.DiscNumber != 2 {
		t.Fatalf("artist album placement was not preserved: %+v", got)
	}
}

func TestTrackMetadataResolverCoalescesPlaylistAlbumReads(t *testing.T) {
	albumWait := make(chan struct{})
	albumStarted := make(chan struct{}, 1)
	catalog := &metadataCountingCatalog{
		metadataSongs: map[string]applemusic.Song{
			"song-1": {ID: "song-1", Name: "One", AlbumID: "album-1"},
			"song-2": {ID: "song-2", Name: "Two", AlbumID: "album-1"},
		},
		albums: map[string]applemusic.Collection{
			"album-1": {
				ID: "album-1", Artist: "Album Artist", ArtworkURL: "album-art", ArtistID: "artist-1", ArtistArtworkURL: "artist-art",
				Tracks: []applemusic.Song{
					{ID: "song-1", TrackCount: 2, DiscNumber: 1, DiscCount: 1},
					{ID: "song-2", TrackCount: 2, DiscNumber: 1, DiscCount: 1},
				},
			},
		},
		metadataCalls: make(map[string]int),
		albumCalls:    make(map[string]int),
		albumWait:     albumWait,
		albumStarted:  albumStarted,
	}
	downloader := &Downloader{cfg: config.Default(), catalog: catalog}
	resolver := newTrackMetadataResolver(downloader, "cn")
	results := make(chan applemusic.Song, 2)
	errs := make(chan error, 2)

	go func() {
		song, err := resolver.song(context.Background(), applemusic.Song{ID: "song-1"}, applemusic.TypePlaylist)
		results <- song
		errs <- err
	}()
	<-albumStarted
	go func() {
		song, err := resolver.song(context.Background(), applemusic.Song{ID: "song-2"}, applemusic.TypePlaylist)
		results <- song
		errs <- err
	}()
	close(albumWait)

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		song := <-results
		if song.AlbumArtist != "Album Artist" || song.AlbumArtworkURL != "album-art" || song.TrackCount != 2 || song.DiscCount != 1 {
			t.Fatalf("playlist song was not enriched: %+v", song)
		}
	}
	_, metadataCalls, albumCalls := catalog.counts()
	if metadataCalls["song-1"] != 1 || metadataCalls["song-2"] != 1 || albumCalls["album-1"] != 1 {
		t.Fatalf("metadata calls = %v, album calls = %v, want one song read each and one shared album read", metadataCalls, albumCalls)
	}
}

func TestTrackMetadataResolverEnrichesStationTracksWithAlbum(t *testing.T) {
	catalog := &metadataCountingCatalog{
		metadataSongs: map[string]applemusic.Song{
			"song-1": {ID: "song-1", Name: "One", AlbumID: "album-1", AlbumArtist: "Album Artist"},
			"song-2": {ID: "song-2", Name: "Two", AlbumID: "album-1", AlbumArtist: "Album Artist"},
		},
		albums: map[string]applemusic.Collection{
			"album-1": {
				ID: "album-1", Artist: "Album Artist", ArtworkURL: "album-art", ArtistID: "artist-1", ArtistArtworkURL: "artist-art",
				Tracks: []applemusic.Song{
					{ID: "song-1", TrackCount: 2, DiscNumber: 2, DiscCount: 2},
					{ID: "song-2", TrackCount: 2, DiscNumber: 1, DiscCount: 2},
				},
			},
		},
		metadataCalls: make(map[string]int),
		albumCalls:    make(map[string]int),
	}
	downloader := &Downloader{cfg: config.Default(), catalog: catalog}
	resolver := newTrackMetadataResolver(downloader, "cn")

	// Station tracks come from the next-tracks feed: their album relationship
	// carries name/artist but no per-track disc totals or album artist id, so
	// they need the same shared album enrichment as playlist tracks.
	for _, id := range []string{"song-1", "song-2"} {
		song, err := resolver.song(context.Background(), applemusic.Song{ID: id}, applemusic.TypeStation)
		if err != nil {
			t.Fatal(err)
		}
		if song.DiscCount != 2 || song.AlbumArtistID != "artist-1" || song.AlbumArtworkURL != "album-art" {
			t.Fatalf("station song %s was not album-enriched: %+v", id, song)
		}
	}
	_, metadataCalls, albumCalls := catalog.counts()
	if metadataCalls["song-1"] != 1 || metadataCalls["song-2"] != 1 || albumCalls["album-1"] != 1 {
		t.Fatalf("metadata calls = %v, album calls = %v, want one song read each and one shared album read", metadataCalls, albumCalls)
	}
}
