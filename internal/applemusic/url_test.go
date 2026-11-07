package applemusic

import "testing"

func TestParseWithAlbumTrackMode(t *testing.T) {
	const albumTrack = "https://music.apple.com/us/album/foo/123456789?i=987654321"
	const classicalAlbumTrack = "https://classical.music.apple.com/us/album/foo/123456789?i=987654321"
	tests := []struct {
		name string
		raw  string
		mode string
		want ParsedURL
	}{
		{"album track as song", albumTrack, "song", ParsedURL{Storefront: "us", Type: TypeSong, ID: "987654321", TrackID: "987654321"}},
		{"album track as album", albumTrack, "album", ParsedURL{Storefront: "us", Type: TypeAlbum, ID: "123456789", TrackID: "987654321"}},
		{"playlist", "https://music.apple.com/jp/playlist/foo/pl.abcdef", "song", ParsedURL{Storefront: "jp", Type: TypePlaylist, ID: "pl.abcdef"}},
		{"station", "https://music.apple.com/us/station/foo/ra.978194965", "song", ParsedURL{Storefront: "us", Type: TypeStation, ID: "ra.978194965"}},
		{"classical album", "https://classical.music.apple.com/cn/album/89286124?l=zh-Hans-CN", "song", ParsedURL{Storefront: "cn", Type: TypeAlbum, ID: "89286124"}},
		{"classical album track as song", classicalAlbumTrack, "song", ParsedURL{Storefront: "us", Type: TypeSong, ID: "987654321", TrackID: "987654321"}},
		{"classical album track as album", classicalAlbumTrack, "album", ParsedURL{Storefront: "us", Type: TypeAlbum, ID: "123456789", TrackID: "987654321"}},
		{"classical song", "https://classical.music.apple.com/gb/song/foo/987654321", "song", ParsedURL{Storefront: "gb", Type: TypeSong, ID: "987654321"}},
		{"classical playlist", "https://classical.music.apple.com/jp/playlist/foo/pl.abcdef", "song", ParsedURL{Storefront: "jp", Type: TypePlaylist, ID: "pl.abcdef"}},
		{"classical artist", "https://classical.music.apple.com/de/artist/foo/123456789", "song", ParsedURL{Storefront: "de", Type: TypeArtist, ID: "123456789"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWithAlbumTrackMode(tt.raw, tt.mode)
			if err != nil {
				t.Fatal(err)
			}
			tt.want.Raw = tt.raw
			if got != tt.want {
				t.Fatalf("parse = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseWithAlbumTrackModeRejects(t *testing.T) {
	tests := []struct{ name, raw, mode string }{
		{"invalid mode", "https://music.apple.com/us/album/foo/123456789?i=987654321", "invalid"},
		{"classical station", "https://classical.music.apple.com/us/station/foo/ra.978194965", "song"},
		{"classical music video", "https://classical.music.apple.com/us/music-video/foo/123456789", "song"},
		{"unknown Apple Music host", "https://unknown.music.apple.com/us/album/foo/123456789", "song"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := ParseWithAlbumTrackMode(tt.raw, tt.mode); err == nil {
				t.Fatalf("parse = %+v, want an error", got)
			}
		})
	}
}

// FuzzParseWithAlbumTrackMode checks that any accepted link names a two-letter
// storefront, a known type and a non-empty id, and keeps the raw input.
func FuzzParseWithAlbumTrackMode(f *testing.F) {
	for _, seed := range []string{
		"https://music.apple.com/us/album/foo/123456789?i=987654321",
		"https://beta.music.apple.com/jp/playlist/foo/pl.abcdef",
		"https://classical.music.apple.com/cn/album/89286124?l=zh-Hans-CN",
		"https://music.apple.com/us/station/foo/ra.978194965",
		"https://music.apple.com/US/music-video/x/1",
		"music.apple.com/us/album/x/1",
	} {
		f.Add(seed, "song")
		f.Add(seed, "album")
	}
	f.Fuzz(func(t *testing.T, raw, mode string) {
		got, err := ParseWithAlbumTrackMode(raw, mode)
		if err != nil {
			return
		}
		if got.Raw != raw || !storefrontPattern.MatchString(got.Storefront) || got.ID == "" {
			t.Fatalf("ParseWithAlbumTrackMode(%q, %q) = %+v", raw, mode, got)
		}
		switch got.Type {
		case TypeSong, TypeAlbum, TypePlaylist, TypeArtist, TypeVideo, TypeStation:
		default:
			t.Fatalf("ParseWithAlbumTrackMode(%q, %q) type = %q", raw, mode, got.Type)
		}
	})
}
