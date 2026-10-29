package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/logging"
)

// CatalogService reads the Apple Music catalog metadata that POST /lookup
// previews. Each method is one light read on the client's catalog host, never
// an amp-api-only read; *applemusic.CatalogClient implements it.
type CatalogService interface {
	SongMetadata(ctx context.Context, storefront, id string) (applemusic.Song, error)
	Album(ctx context.Context, storefront, id string) (applemusic.Collection, error)
	Playlist(ctx context.Context, storefront, id, mediaUserToken string) (applemusic.Collection, error)
	Station(ctx context.Context, storefront, id string) (applemusic.StationInfo, error)
	Artist(ctx context.Context, storefront, id string) (applemusic.Artist, error)
}

// lookupTimeout bounds one lookup: the album and playlist reads page through
// their track lists, and a preview that takes longer is no longer a preview.
const lookupTimeout = 15 * time.Second

// lookupLinkFields is the parsed link: the LookupLink schema, also sent as
// details.link when a lookup fails after the link parsed.
type lookupLinkFields struct {
	Input      string             `json:"input"`
	Type       applemusic.URLType `json:"type"`
	Storefront string             `json:"storefront"`
	ID         string             `json:"id"`
	TrackID    string             `json:"track_id,omitempty"`
}

// lookupResponse is the LookupResponse schema. The metadata fields carry the
// names and meaning of the matching Job fields.
type lookupResponse struct {
	lookupLinkFields
	Title               string `json:"title"`
	ArtistName          string `json:"artist_name,omitempty"`
	ArtistURL           string `json:"artist_url,omitempty"`
	CuratorName         string `json:"curator_name,omitempty"`
	AlbumName           string `json:"album_name,omitempty"`
	ArtworkURL          string `json:"artwork_url,omitempty"`
	ArtworkBgColor      string `json:"artwork_bg_color,omitempty"`
	ArtworkTextColor1   string `json:"artwork_text_color1,omitempty"`
	ArtworkTextColor2   string `json:"artwork_text_color2,omitempty"`
	ArtworkTextColor3   string `json:"artwork_text_color3,omitempty"`
	ArtworkTextColor4   string `json:"artwork_text_color4,omitempty"`
	ReleaseDate         string `json:"release_date,omitempty"`
	Genre               string `json:"genre,omitempty"`
	TrackCount          int    `json:"track_count,omitempty"`
	DurationMS          int    `json:"duration_ms,omitempty"`
	ContentRating       string `json:"content_rating,omitempty"`
	AppleDigitalMaster  bool   `json:"apple_digital_master,omitempty"`
	NeedsMediaUserToken bool   `json:"needs_media_user_token,omitempty"`
}

func (r *lookupResponse) setArtwork(url string, colors applemusic.ArtworkColors) {
	r.ArtworkURL = url
	r.ArtworkBgColor = colors.BgColor
	r.ArtworkTextColor1 = colors.TextColor1
	r.ArtworkTextColor2 = colors.TextColor2
	r.ArtworkTextColor3 = colors.TextColor3
	r.ArtworkTextColor4 = colors.TextColor4
}

// notDownloadableError is a link that parses and resolves but that no job can
// download (a live radio station); the lookup answers 400 with its message.
type notDownloadableError struct{ message string }

func (e notDownloadableError) Error() string { return e.message }

// lookupLink previews one Apple Music link without creating a job: the link as
// a submission would parse it, plus the catalog metadata a job row shows.
func (s *Server) lookupLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	input := strings.TrimSpace(req.URL)
	if input == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "url is required")
		return
	}
	cfg := s.currentConfig()
	parsed, err := applemusic.ParseWithAlbumTrackMode(input, cfg.Catalog.AlbumTrackURLMode)
	if err != nil {
		// Same split as the submission validator: a bad mode is the server's
		// config (validated at load, so unexpected), anything else the link.
		if strings.Contains(err.Error(), "album_track_url_mode") {
			writeError(w, http.StatusInternalServerError, CodeInternalError, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "not a supported Apple Music link: "+err.Error())
		return
	}
	link := lookupLinkFields{Input: input, Type: parsed.Type, Storefront: parsed.Storefront, ID: parsed.ID, TrackID: parsed.TrackID}
	details := map[string]any{"link": link}
	if parsed.Type == applemusic.TypeVideo {
		writeErrorDetails(w, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("%s download is not implemented", parsed.Type), details)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	resp, err := s.previewLink(ctx, parsed, strings.TrimSpace(cfg.Catalog.MediaUserToken))
	switch {
	case err == nil:
		resp.lookupLinkFields = link
		writeJSON(w, http.StatusOK, resp)
	case errors.As(err, new(notDownloadableError)):
		writeErrorDetails(w, http.StatusBadRequest, CodeInvalidRequest, err.Error(), details)
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		writeErrorDetails(w, http.StatusGatewayTimeout, CodeUpstreamTimeout,
			fmt.Sprintf("the Apple Music catalog did not answer within %s", lookupTimeout), details)
	default:
		// The response carries only err.Error(); a raw upstream body an
		// Apple error holds stays in the log line.
		attrs := []any{"link", link.Input, "error", err}
		if body := logging.UpstreamBody(err); body != "" {
			attrs = append(attrs, "upstream_body", body)
		}
		logging.FromContext(r.Context(), s.logger).Warn("lookup failed", attrs...)
		writeErrorDetails(w, http.StatusBadGateway, CodeUpstreamError, "read Apple Music catalog: "+err.Error(), details)
	}
}

// previewLink reads the catalog metadata of parsed, filling the display fields
// the way the job pipeline fills a job's (internal/media resolveCollection),
// but from the lightest read that has them.
func (s *Server) previewLink(ctx context.Context, parsed applemusic.ParsedURL, mediaUserToken string) (lookupResponse, error) {
	var resp lookupResponse
	switch parsed.Type {
	case applemusic.TypeSong:
		song, err := s.catalog.SongMetadata(ctx, parsed.Storefront, parsed.ID)
		if err != nil {
			return resp, err
		}
		resp.Title, resp.ArtistName, resp.ArtistURL, resp.AlbumName = song.Name, song.ArtistName, song.ArtistURL, song.AlbumName
		resp.setArtwork(song.DisplayArtwork())
		resp.ReleaseDate, resp.Genre = song.ReleaseDate, applemusic.PrimaryGenre(song.GenreNames)
		resp.TrackCount, resp.DurationMS = 1, song.DurationInMillis
		resp.ContentRating, resp.AppleDigitalMaster = song.ContentRating, song.AppleDigitalMaster
	case applemusic.TypeAlbum:
		album, err := s.catalog.Album(ctx, parsed.Storefront, parsed.ID)
		if err != nil {
			return resp, err
		}
		resp.Title, resp.ArtistName, resp.ArtistURL = album.Name, album.Artist, album.ArtistURL
		resp.setArtwork(album.ArtworkURL, album.ArtworkColors)
		resp.ReleaseDate, resp.Genre = album.ReleaseDate, applemusic.PrimaryGenre(album.GenreNames)
		resp.TrackCount = len(album.Tracks)
		resp.ContentRating, resp.AppleDigitalMaster = album.ContentRating, album.AppleDigitalMaster
	case applemusic.TypePlaylist:
		playlist, err := s.catalog.Playlist(ctx, parsed.Storefront, parsed.ID, mediaUserToken)
		if err != nil {
			return resp, err
		}
		resp.Title, resp.CuratorName = playlist.Name, playlist.Artist
		resp.setArtwork(playlist.ArtworkURL, playlist.ArtworkColors)
		resp.TrackCount = len(playlist.Tracks)
	case applemusic.TypeStation:
		// Station metadata only: the track list comes from next-tracks, a
		// rolling feed that needs the media-user-token and that a preview
		// must not advance.
		station, err := s.catalog.Station(ctx, parsed.Storefront, parsed.ID)
		if err != nil {
			return resp, err
		}
		if !station.Downloadable() {
			return resp, notDownloadableError{fmt.Sprintf("station %q is a live radio stream; only track-based stations download", station.Name)}
		}
		resp.Title, resp.CuratorName = station.Name, applemusic.StationCuratorName
		resp.setArtwork(station.ArtworkURL, station.ArtworkColors)
		resp.NeedsMediaUserToken = true
	case applemusic.TypeArtist:
		// The artist alone: counting its tracks would read every album.
		artist, err := s.catalog.Artist(ctx, parsed.Storefront, parsed.ID)
		if err != nil {
			return resp, err
		}
		resp.Title, resp.ArtistName, resp.ArtistURL = artist.Name, artist.Name, artist.URL
		resp.setArtwork(artist.ArtworkURL, artist.ArtworkColors)
		resp.Genre = applemusic.PrimaryGenre(artist.GenreNames)
	default:
		return resp, notDownloadableError{fmt.Sprintf("%s links have no preview", parsed.Type)}
	}
	return resp, nil
}
