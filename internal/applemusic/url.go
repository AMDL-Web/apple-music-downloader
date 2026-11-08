package applemusic

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type URLType string

const (
	TypeSong     URLType = "song"
	TypeAlbum    URLType = "album"
	TypePlaylist URLType = "playlist"
	TypeArtist   URLType = "artist"
	TypeVideo    URLType = "music-video"
	TypeStation  URLType = "station"
)

type ParsedURL struct {
	Raw        string  `json:"raw"`
	Storefront string  `json:"storefront"`
	Type       URLType `json:"type"`
	ID         string  `json:"id"`
	// TrackID is the ?i= song id of an album link, whichever Type the album
	// track mode picked: with mode song it equals ID, with mode album it names
	// the track the link pointed at inside the album. Empty for other links.
	TrackID string `json:"track_id,omitempty"`
}

var storefrontPattern = regexp.MustCompile(`^[a-z]{2}$`)

const (
	appleMusicHost          = "music.apple.com"
	betaAppleMusicHost      = "beta.music.apple.com"
	classicalAppleMusicHost = "classical.music.apple.com"
)

// linkShape is what a parse error says a link should look like.
const linkShape = "expected https://music.apple.com/{storefront}/{album|song|playlist|artist|station|music-video}/..."

func ParseWithAlbumTrackMode(raw, albumTrackURLMode string) (ParsedURL, error) {
	if albumTrackURLMode != "song" && albumTrackURLMode != "album" {
		return ParsedURL{}, fmt.Errorf("invalid album_track_url_mode %q (expected song or album)", albumTrackURLMode)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ParsedURL{}, err
	}
	if u.Host == "" {
		return ParsedURL{}, fmt.Errorf("not an http(s) URL; %s", linkShape)
	}
	if u.Host != appleMusicHost && u.Host != betaAppleMusicHost && u.Host != classicalAppleMusicHost {
		return ParsedURL{}, fmt.Errorf("unsupported host %q; %s", u.Host, linkShape)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 {
		return ParsedURL{}, fmt.Errorf("invalid Apple Music URL")
	}
	storefront := strings.ToLower(parts[0])
	if !storefrontPattern.MatchString(storefront) {
		return ParsedURL{}, fmt.Errorf("invalid storefront %q", storefront)
	}
	kind := URLType(parts[1])
	if u.Host == classicalAppleMusicHost && (kind == TypeVideo || kind == TypeStation) {
		return ParsedURL{}, fmt.Errorf("unsupported Apple Music Classical URL type %q", kind)
	}
	id := parts[len(parts)-1]
	switch kind {
	case TypeAlbum:
		songID := u.Query().Get("i")
		if songID != "" && albumTrackURLMode == "song" {
			return ParsedURL{Raw: raw, Storefront: storefront, Type: TypeSong, ID: songID, TrackID: songID}, nil
		}
		return ParsedURL{Raw: raw, Storefront: storefront, Type: TypeAlbum, ID: id, TrackID: songID}, nil
	case TypeSong:
		return ParsedURL{Raw: raw, Storefront: storefront, Type: TypeSong, ID: id}, nil
	case TypePlaylist:
		return ParsedURL{Raw: raw, Storefront: storefront, Type: TypePlaylist, ID: id}, nil
	case TypeArtist:
		return ParsedURL{Raw: raw, Storefront: storefront, Type: TypeArtist, ID: id}, nil
	case TypeStation:
		return ParsedURL{Raw: raw, Storefront: storefront, Type: TypeStation, ID: id}, nil
	case TypeVideo:
		return ParsedURL{Raw: raw, Storefront: storefront, Type: TypeVideo, ID: id}, nil
	default:
		return ParsedURL{}, fmt.Errorf("unsupported Apple Music URL type %q", kind)
	}
}
