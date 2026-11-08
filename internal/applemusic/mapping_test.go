package applemusic

import (
	"reflect"
	"testing"
)

// TestEnrichWithAlbumCopiesEveryAlbumField pins the complete set of fields
// EnrichWithAlbum writes. A field added to the album side later shows up here as
// a deliberate decision rather than as a silent difference between callers.
func TestEnrichWithAlbumCopiesEveryAlbumField(t *testing.T) {
	album := Collection{
		ID:               "album-1",
		Type:             TypeAlbum,
		Name:             "Album",
		Artist:           "Album Artist",
		ArtworkURL:       "album-art/{w}x{h}bb.jpg",
		ArtworkColors:    ArtworkColors{BgColor: "102030", TextColor1: "a0a0a0", TextColor2: "b0b0b0", TextColor3: "c0c0c0", TextColor4: "d0d0d0"},
		ArtistID:         "artist-1",
		ArtistArtworkURL: "artist-art/{w}x{h}bb.jpg",
		ArtistURL:        "https://music.apple.com/us/artist/album-artist/artist-1",
		ReleaseDate:      "2024-01-02",
		GenreNames:       []string{"Pop"},
		Tracks: []Song{
			{ID: "song-1", DiscNumber: 1, TrackNumber: 1, TrackCount: 12, DiscCount: 2},
			{ID: "song-2", DiscNumber: 2, TrackNumber: 1, TrackCount: 9, DiscCount: 2},
		},
	}

	tests := []struct {
		name   string
		song   Song
		tracks []Song // replaces album.Tracks when set
		want   Song
	}{
		{
			name: "empty song takes every album field",
			song: Song{ID: "song-2", DiscNumber: 2},
			want: Song{
				ID:                    "song-2",
				DiscNumber:            2,
				TrackCount:            9,
				DiscCount:             2,
				AlbumArtworkURL:       "album-art/{w}x{h}bb.jpg",
				AlbumArtworkColors:    ArtworkColors{BgColor: "102030", TextColor1: "a0a0a0", TextColor2: "b0b0b0", TextColor3: "c0c0c0", TextColor4: "d0d0d0"},
				AlbumArtist:           "Album Artist",
				AlbumArtistID:         "artist-1",
				AlbumArtistArtworkURL: "artist-art/{w}x{h}bb.jpg",
				ArtistURL:             "https://music.apple.com/us/artist/album-artist/artist-1",
			},
		},
		{
			name: "song's own artist page is kept",
			song: Song{ID: "song-1", DiscNumber: 1, ArtistURL: "https://music.apple.com/us/artist/featured/artist-9"},
			want: Song{
				ID:                    "song-1",
				DiscNumber:            1,
				TrackCount:            12,
				DiscCount:             2,
				AlbumArtworkURL:       "album-art/{w}x{h}bb.jpg",
				AlbumArtworkColors:    ArtworkColors{BgColor: "102030", TextColor1: "a0a0a0", TextColor2: "b0b0b0", TextColor3: "c0c0c0", TextColor4: "d0d0d0"},
				AlbumArtist:           "Album Artist",
				AlbumArtistID:         "artist-1",
				AlbumArtistArtworkURL: "artist-art/{w}x{h}bb.jpg",
				ArtistURL:             "https://music.apple.com/us/artist/featured/artist-9",
			},
		},
		{
			// The album relationship can omit a song (storefront-withheld
			// track): disc 1 then has track numbers 1 and 3, so at least three
			// tracks, and the album spans two discs.
			name: "song missing from the album's track list derives its totals",
			song: Song{ID: "song-b", DiscNumber: 1},
			tracks: []Song{
				{ID: "song-a", DiscNumber: 1, TrackNumber: 1},
				{ID: "song-c", DiscNumber: 1, TrackNumber: 3},
				{ID: "song-d", DiscNumber: 2, TrackNumber: 1},
			},
			want: Song{
				ID:                    "song-b",
				DiscNumber:            1,
				TrackCount:            3,
				DiscCount:             2,
				AlbumArtworkURL:       "album-art/{w}x{h}bb.jpg",
				AlbumArtworkColors:    ArtworkColors{BgColor: "102030", TextColor1: "a0a0a0", TextColor2: "b0b0b0", TextColor3: "c0c0c0", TextColor4: "d0d0d0"},
				AlbumArtist:           "Album Artist",
				AlbumArtistID:         "artist-1",
				AlbumArtistArtworkURL: "artist-art/{w}x{h}bb.jpg",
				ArtistURL:             "https://music.apple.com/us/artist/album-artist/artist-1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := album
			if tt.tracks != nil {
				in.Tracks = tt.tracks
			}
			if got := EnrichWithAlbum(tt.song, in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EnrichWithAlbum =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestMaxDisc(t *testing.T) {
	tests := []struct {
		name   string
		tracks []Song
		want   int
	}{
		{"no tracks is one disc", nil, 1},
		{"missing disc numbers are one disc", []Song{{}, {}}, 1},
		{"highest disc wins", []Song{{DiscNumber: 1}, {DiscNumber: 3}, {DiscNumber: 2}}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maxDisc(tt.tracks); got != tt.want {
				t.Fatalf("maxDisc = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPrimaryGenreSkipsMusicUmbrella(t *testing.T) {
	tests := []struct {
		names []string
		want  string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"Music"}, ""},
		{[]string{"Music", "Pop"}, "Pop"},
		{[]string{"Hip-Hop/Rap", "Music"}, "Hip-Hop/Rap"},
		{[]string{"", "Music", "Jazz"}, "Jazz"},
	}
	for _, tt := range tests {
		if got := PrimaryGenre(tt.names); got != tt.want {
			t.Fatalf("PrimaryGenre(%v) = %q, want %q", tt.names, got, tt.want)
		}
	}
}

// The palette must describe the image that is shown, so it follows the same
// song-then-album fallback as the URL.
func TestSongDisplayArtworkKeepsURLAndPaletteTogether(t *testing.T) {
	own := Song{ArtworkURL: "song-art", ArtworkColors: ArtworkColors{BgColor: "111111"}, AlbumArtworkURL: "album-art", AlbumArtworkColors: ArtworkColors{BgColor: "222222"}}
	if url, colors := own.DisplayArtwork(); url != "song-art" || colors.BgColor != "111111" {
		t.Fatalf("own artwork = %q %+v", url, colors)
	}
	albumOnly := Song{AlbumArtworkURL: "album-art", ArtworkColors: ArtworkColors{BgColor: "111111"}, AlbumArtworkColors: ArtworkColors{BgColor: "222222"}}
	if url, colors := albumOnly.DisplayArtwork(); url != "album-art" || colors.BgColor != "222222" {
		t.Fatalf("album fallback = %q %+v", url, colors)
	}
}
