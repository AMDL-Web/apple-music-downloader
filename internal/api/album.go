package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"

	"amdl/internal/applemusic"
)

// albumResponse is the AlbumResponse schema: the album's metadata under the
// names and meaning of the LookupResponse fields, then its songs.
type albumResponse struct {
	Storefront         string       `json:"storefront"`
	ID                 string       `json:"id"`
	URL                string       `json:"url"`
	Title              string       `json:"title"`
	ArtistName         string       `json:"artist_name,omitempty"`
	ArtistURL          string       `json:"artist_url,omitempty"`
	ArtworkURL         string       `json:"artwork_url,omitempty"`
	ArtworkBgColor     string       `json:"artwork_bg_color,omitempty"`
	ArtworkTextColor1  string       `json:"artwork_text_color1,omitempty"`
	ArtworkTextColor2  string       `json:"artwork_text_color2,omitempty"`
	ArtworkTextColor3  string       `json:"artwork_text_color3,omitempty"`
	ArtworkTextColor4  string       `json:"artwork_text_color4,omitempty"`
	ReleaseDate        string       `json:"release_date,omitempty"`
	Genre              string       `json:"genre,omitempty"`
	TrackCount         int          `json:"track_count"`
	DiscCount          int          `json:"disc_count,omitempty"`
	ContentRating      string       `json:"content_rating,omitempty"`
	AppleDigitalMaster bool         `json:"apple_digital_master,omitempty"`
	AudioTraits        []string     `json:"audio_traits,omitempty"`
	Tracks             []albumTrack `json:"tracks"`
}

// albumTrack is the AlbumTrack schema: one song of the album. The metadata
// fields have the names and meaning of the SearchSong fields of the same name.
type albumTrack struct {
	ID                 string   `json:"id"`
	URL                string   `json:"url"`
	DiscNumber         int      `json:"disc_number"`
	TrackNumber        int      `json:"track_number"`
	Name               string   `json:"name"`
	ArtistName         string   `json:"artist_name,omitempty"`
	DurationMS         int      `json:"duration_ms,omitempty"`
	ContentRating      string   `json:"content_rating,omitempty"`
	AppleDigitalMaster bool     `json:"apple_digital_master,omitempty"`
	AudioTraits        []string `json:"audio_traits,omitempty"`
	HasLyrics          bool     `json:"has_lyrics,omitempty"`
}

// getAlbum answers GET /albums/{album_id}: the album's metadata and its song
// list, the songs an album download takes, in disc and track order.
func (s *Server) getAlbum(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("album_id")
	if !catalogIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "album_id must be an Apple Music catalog id (digits)")
		return
	}
	storefront, err := s.browseStorefront(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	album, err := s.catalog.Album(ctx, storefront, id)
	if err != nil {
		s.writeCatalogError(ctx, w, r, err, lookupTimeout, nil, "album failed", "album_id", id, "storefront", storefront)
		return
	}
	writeJSON(w, http.StatusOK, newAlbumResponse(storefront, id, album))
}

func newAlbumResponse(storefront, id string, album applemusic.Collection) albumResponse {
	resp := albumResponse{
		Storefront: storefront, ID: id, URL: hitURL(applemusic.TypeAlbum, storefront, id, album.URL),
		Title: album.Name, ArtistName: album.Artist, ArtistURL: album.ArtistURL,
		ArtworkURL: album.ArtworkURL, ArtworkBgColor: album.ArtworkColors.BgColor,
		ArtworkTextColor1: album.ArtworkColors.TextColor1, ArtworkTextColor2: album.ArtworkColors.TextColor2,
		ArtworkTextColor3: album.ArtworkColors.TextColor3, ArtworkTextColor4: album.ArtworkColors.TextColor4,
		ReleaseDate: album.ReleaseDate, Genre: applemusic.PrimaryGenre(album.GenreNames),
		TrackCount: len(album.Tracks), ContentRating: album.ContentRating,
		AppleDigitalMaster: album.AppleDigitalMaster, AudioTraits: album.AudioTraits,
		Tracks: make([]albumTrack, 0, len(album.Tracks)),
	}
	for _, song := range album.Tracks {
		disc := max(1, song.DiscNumber)
		resp.DiscCount = max(resp.DiscCount, disc)
		resp.Tracks = append(resp.Tracks, albumTrack{
			ID: song.ID, URL: hitURL(applemusic.TypeSong, storefront, song.ID, song.URL),
			DiscNumber: disc, TrackNumber: song.TrackNumber, Name: song.Name, ArtistName: song.ArtistName,
			DurationMS: song.DurationInMillis, ContentRating: song.ContentRating,
			AppleDigitalMaster: song.AppleDigitalMaster, AudioTraits: song.AudioTraits, HasLyrics: song.HasLyrics,
		})
	}
	slices.SortStableFunc(resp.Tracks, func(a, b albumTrack) int {
		return cmp.Or(cmp.Compare(a.DiscNumber, b.DiscNumber), cmp.Compare(a.TrackNumber, b.TrackNumber))
	})
	return resp
}
