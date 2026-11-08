package api

import (
	"context"
	"net/http"
	"regexp"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
)

// playlistIDPattern is a catalog playlist id: "pl." and Apple's letters,
// digits and dashes, "pl.u-" for one a person shared.
var playlistIDPattern = regexp.MustCompile(`^pl\.[A-Za-z0-9._-]{1,200}$`)

// getPlaylist answers GET /playlists/{playlist_id}: the playlist's metadata
// and its song list, the songs a playlist download takes, in its order, in
// the AlbumResponse shape (PlaylistResponse).
func (s *Server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("playlist_id")
	if !playlistIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "playlist_id must be an Apple Music catalog playlist id (pl.…)")
		return
	}
	storefront, err := s.browseStorefront(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	playlist, err := s.catalog.Playlist(ctx, storefront, id, s.tokens.TokenFor(s.currentConfig(), storefront).Value)
	if err != nil {
		s.writeCatalogError(ctx, w, r, err, lookupTimeout, nil, "playlist failed", "playlist_id", id, "storefront", storefront)
		return
	}
	resp := newPlaylistResponse(storefront, id, playlist)
	if resp.Resource = s.resourceMark(ctx, applemusic.TypePlaylist, id); resp.Resource != nil {
		if states, err := s.store.RecordTrackStates(ctx, domain.ResourcePlaylist, id); err == nil {
			for i := range resp.Tracks {
				resp.Tracks[i].State = states[resp.Tracks[i].ID]
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// newPlaylistResponse builds the answer: the curator as the artist, each
// song on disc 1 at its place in the list.
func newPlaylistResponse(storefront, id string, playlist applemusic.Collection) albumResponse {
	resp := albumResponse{
		Storefront: storefront, ID: id, URL: hitURL(applemusic.TypePlaylist, storefront, id, playlist.URL),
		Title: playlist.Name, ArtistName: playlist.Artist,
		ArtworkURL: playlist.ArtworkURL, ArtworkBgColor: playlist.ArtworkColors.BgColor,
		ArtworkTextColor1: playlist.ArtworkColors.TextColor1, ArtworkTextColor2: playlist.ArtworkColors.TextColor2,
		ArtworkTextColor3: playlist.ArtworkColors.TextColor3, ArtworkTextColor4: playlist.ArtworkColors.TextColor4,
		TrackCount: len(playlist.Tracks), AudioTraits: playlist.AudioTraits,
		Tracks: make([]albumTrack, 0, len(playlist.Tracks)),
	}
	if len(playlist.Tracks) > 0 {
		resp.DiscCount = 1
	}
	for i, song := range playlist.Tracks {
		resp.Tracks = append(resp.Tracks, albumTrack{
			ID: song.ID, URL: hitURL(applemusic.TypeSong, storefront, song.ID, song.URL),
			DiscNumber: 1, TrackNumber: i + 1, Name: song.Name, ArtistName: song.ArtistName,
			DurationMS: song.DurationInMillis, ContentRating: song.ContentRating,
			AppleDigitalMaster: song.AppleDigitalMaster, AudioTraits: song.AudioTraits, HasLyrics: song.HasLyrics,
		})
	}
	return resp
}
