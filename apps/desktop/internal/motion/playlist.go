// Package motion plays Apple Music's animated covers: it reads the HLS
// master playlist a job carries, fetches one rendition sized for the view
// into the cache once, and decodes it with ffmpeg into frames for
// ui.VideoPlayer, looping from the local file.
package motion

import (
	"bufio"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Variant is one video rendition of a master playlist.
type Variant struct {
	URL           string
	Width, Height int
	Codecs        string
	// Bandwidth is the average bit rate, or the peak one when the playlist
	// gives no average.
	Bandwidth int
	// FrameRate is 0 when the playlist does not say.
	FrameRate float64
}

// ParseMaster reads the video renditions of a master playlist, resolving
// their URIs against base. I-frame (trick play) renditions are left out.
func ParseMaster(text string, base *url.URL) []Variant {
	var out []Variant
	var pending *Variant
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			a := parseAttrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			v := Variant{Codecs: a["CODECS"]}
			v.Width, v.Height = parseResolution(a["RESOLUTION"])
			v.Bandwidth, _ = strconv.Atoi(a["AVERAGE-BANDWIDTH"])
			if v.Bandwidth == 0 {
				v.Bandwidth, _ = strconv.Atoi(a["BANDWIDTH"])
			}
			v.FrameRate, _ = strconv.ParseFloat(a["FRAME-RATE"], 64)
			pending = &v
		case line == "" || strings.HasPrefix(line, "#"):
		case pending != nil:
			if u, err := base.Parse(line); err == nil {
				pending.URL = u.String()
				out = append(out, *pending)
			}
			pending = nil
		}
	}
	return out
}

// Pick chooses the rendition to show at least px pixels on its shorter
// side: among H.264 renditions when there are any (every ffmpeg build
// decodes them), the smallest that is big enough, the best encoded of
// those; the largest when none is big enough.
func Pick(vs []Variant, px int) (Variant, bool) {
	var avc []Variant
	for _, v := range vs {
		if strings.HasPrefix(v.Codecs, "avc1") {
			avc = append(avc, v)
		}
	}
	if len(avc) > 0 {
		vs = avc
	}
	short := func(v Variant) int { return min(v.Width, v.Height) }
	var best Variant
	found := false
	for _, v := range vs {
		if short(v) < px {
			continue
		}
		if !found || short(v) < short(best) || (short(v) == short(best) && v.Bandwidth > best.Bandwidth) {
			best, found = v, true
		}
	}
	if found {
		return best, true
	}
	for _, v := range vs {
		if !found || short(v) > short(best) || (short(v) == short(best) && v.Bandwidth > best.Bandwidth) {
			best, found = v, true
		}
	}
	return best, found
}

// piece is a byte range of a media file: n < 0 is the whole file.
type piece struct {
	url    string
	off, n int64
}

// parseMedia reads the initialization section and the segments of a media
// playlist, in order, merging ranges that continue one another in the
// same file. Apple's renditions are one fragmented MP4 cut into ranges, so
// this usually comes back as a single piece.
func parseMedia(text string, base *url.URL) ([]piece, error) {
	var out []piece
	add := func(uri, rng string) error {
		u, err := base.Parse(uri)
		if err != nil {
			return err
		}
		p := piece{url: u.String(), n: -1}
		if rng != "" {
			n, off, ok := strings.Cut(rng, "@")
			if p.n, err = strconv.ParseInt(n, 10, 64); err != nil {
				return err
			}
			switch {
			case ok:
				if p.off, err = strconv.ParseInt(off, 10, 64); err != nil {
					return err
				}
			case len(out) > 0 && out[len(out)-1].url == p.url && out[len(out)-1].n >= 0:
				// A range without an offset follows the previous one.
				last := out[len(out)-1]
				p.off = last.off + last.n
			}
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.url == p.url && last.n >= 0 && p.n >= 0 && last.off+last.n == p.off {
				last.n += p.n
				return nil
			}
		}
		out = append(out, p)
		return nil
	}
	rng := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "#EXT-X-KEY:") && !strings.Contains(line, "METHOD=NONE"):
			return nil, errors.New("motion: encrypted rendition")
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			a := parseAttrs(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			if err := add(a["URI"], a["BYTERANGE"]); err != nil {
				return nil, err
			}
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			rng = strings.TrimPrefix(line, "#EXT-X-BYTERANGE:")
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			if err := add(line, rng); err != nil {
				return nil, err
			}
			rng = ""
		}
	}
	if len(out) == 0 {
		return nil, errors.New("motion: a media playlist with no segments")
	}
	return out, nil
}

// parseAttrs reads an HLS attribute list; quoted values keep their commas.
func parseAttrs(s string) map[string]string {
	out := map[string]string{}
	for s != "" {
		key, rest, ok := strings.Cut(s, "=")
		if !ok {
			break
		}
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				val, rest = rest[1:], ""
			} else {
				val, rest = rest[1:1+end], rest[2+end:]
			}
			rest = strings.TrimPrefix(rest, ",")
		} else {
			val, rest, _ = strings.Cut(rest, ",")
		}
		out[strings.TrimSpace(key)] = val
		s = rest
	}
	return out
}

func parseResolution(s string) (w, h int) {
	ws, hs, ok := strings.Cut(s, "x")
	if !ok {
		return 0, 0
	}
	w, _ = strconv.Atoi(ws)
	h, _ = strconv.Atoi(hs)
	return w, h
}
