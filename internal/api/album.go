package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
)

// albumResponse is the AlbumResponse schema: the album's metadata under the
// names and meaning of the LookupResponse fields, how much of it is on disk,
// then its songs.
type albumResponse struct {
	Storefront         string   `json:"storefront"`
	ID                 string   `json:"id"`
	URL                string   `json:"url"`
	Title              string   `json:"title"`
	ArtistName         string   `json:"artist_name,omitempty"`
	ArtistURL          string   `json:"artist_url,omitempty"`
	ArtworkURL         string   `json:"artwork_url,omitempty"`
	ArtworkBgColor     string   `json:"artwork_bg_color,omitempty"`
	ArtworkTextColor1  string   `json:"artwork_text_color1,omitempty"`
	ArtworkTextColor2  string   `json:"artwork_text_color2,omitempty"`
	ArtworkTextColor3  string   `json:"artwork_text_color3,omitempty"`
	ArtworkTextColor4  string   `json:"artwork_text_color4,omitempty"`
	ReleaseDate        string   `json:"release_date,omitempty"`
	Genre              string   `json:"genre,omitempty"`
	TrackCount         int      `json:"track_count"`
	DiscCount          int      `json:"disc_count,omitempty"`
	ContentRating      string   `json:"content_rating,omitempty"`
	AppleDigitalMaster bool     `json:"apple_digital_master,omitempty"`
	AudioTraits        []string `json:"audio_traits,omitempty"`
	// MotionArtwork* are the album's animated cover and its palette, with the
	// names and meaning of the Job fields of the same name, read alongside
	// the album (albumMotion): absent when it has none or the read did not
	// answer in time.
	MotionArtworkURL        string `json:"motion_artwork_url,omitempty"`
	MotionArtworkTallURL    string `json:"motion_artwork_tall_url,omitempty"`
	MotionArtworkBgColor    string `json:"motion_artwork_bg_color,omitempty"`
	MotionArtworkTextColor1 string `json:"motion_artwork_text_color1,omitempty"`
	MotionArtworkTextColor2 string `json:"motion_artwork_text_color2,omitempty"`
	MotionArtworkTextColor3 string `json:"motion_artwork_text_color3,omitempty"`
	MotionArtworkTextColor4 string `json:"motion_artwork_text_color4,omitempty"`
	// Library is not set since 2.0, which stats no file before a download.
	// TODO(P3): the album's resource replaces it.
	Library *domain.LibraryState `json:"library,omitempty"`
	Tracks  []albumTrack         `json:"tracks"`
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
	// OnDisk is not set since 2.0. TODO(P3): the track's state replaces it.
	OnDisk *bool `json:"on_disk,omitempty"`
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
	motion := s.albumMotion(ctx, storefront, id)
	album, err := s.catalog.Album(ctx, storefront, id)
	if err != nil {
		s.writeCatalogError(ctx, w, r, err, lookupTimeout, nil, "album failed", "album_id", id, "storefront", storefront)
		return
	}
	resp := newAlbumResponse(storefront, id, album)
	if art := motion(); !art.IsZero() {
		resp.MotionArtworkURL, resp.MotionArtworkTallURL = art.Square, art.Tall
		resp.MotionArtworkBgColor, resp.MotionArtworkTextColor1 = art.SquareColors.BgColor, art.SquareColors.TextColor1
		resp.MotionArtworkTextColor2, resp.MotionArtworkTextColor3 = art.SquareColors.TextColor2, art.SquareColors.TextColor3
		resp.MotionArtworkTextColor4 = art.SquareColors.TextColor4
	}
	writeJSON(w, http.StatusOK, resp)
}

// albumMotionWait is how long GET /albums/{id} waits for the album's
// animated cover once the album itself is read: the cover is decoration,
// read alongside, and never holds the answer up for long.
const albumMotionWait = 1500 * time.Millisecond

// motionArtworkReader reads an album's animated cover, an optional read
// (applemusic.CatalogClient.MotionArtworkViaWebToken).
type motionArtworkReader interface {
	MotionArtworkViaWebToken(ctx context.Context, storefront, id string) (applemusic.MotionArtwork, error)
}

// albumMotion starts reading album id's animated cover beside the album's
// own read and returns the wait for it. The wait gives the zero
// MotionArtwork, no cover, when catalog.motion_artwork_enabled is off, the
// catalog has no such read, the read fails, or it has not answered
// albumMotionWait after the wait starts, once the album is read: it is
// optional, and its failure is "not available", never the album's.
func (s *Server) albumMotion(ctx context.Context, storefront, id string) func() applemusic.MotionArtwork {
	reader, ok := s.catalog.(motionArtworkReader)
	if !ok || !s.currentConfig().Catalog.MotionArtworkEnabled {
		return func() applemusic.MotionArtwork { return applemusic.MotionArtwork{} }
	}
	got := make(chan applemusic.MotionArtwork, 1)
	go func() {
		// An album without an animated cover answers an error too; either
		// way there is none to show.
		art, err := reader.MotionArtworkViaWebToken(ctx, storefront, id)
		if err != nil {
			art = applemusic.MotionArtwork{}
		}
		got <- art
	}()
	return func() applemusic.MotionArtwork {
		wait := time.NewTimer(albumMotionWait)
		defer wait.Stop()
		select {
		case art := <-got:
			return art
		case <-wait.C:
		case <-ctx.Done():
		}
		return applemusic.MotionArtwork{}
	}
}

// newAlbumResponse builds the answer.
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
		track := albumTrack{
			ID: song.ID, URL: hitURL(applemusic.TypeSong, storefront, song.ID, song.URL),
			DiscNumber: disc, TrackNumber: song.TrackNumber, Name: song.Name, ArtistName: song.ArtistName,
			DurationMS: song.DurationInMillis, ContentRating: song.ContentRating,
			AppleDigitalMaster: song.AppleDigitalMaster, AudioTraits: song.AudioTraits, HasLyrics: song.HasLyrics,
		}
		resp.Tracks = append(resp.Tracks, track)
	}
	slices.SortStableFunc(resp.Tracks, func(a, b albumTrack) int {
		return cmp.Or(cmp.Compare(a.DiscNumber, b.DiscNumber), cmp.Compare(a.TrackNumber, b.TrackNumber))
	})
	return resp
}
