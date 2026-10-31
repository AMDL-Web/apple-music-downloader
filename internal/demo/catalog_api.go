package demo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"amdl/internal/client"
)

func (b *Backend) lookup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the body has no url")
		return
	}
	a, one, ok := resolve(body.URL)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_url", "not an Apple Music link")
		return
	}
	l := client.Lookup{Input: body.URL, Type: "album", Storefront: "us", ID: a.id, Title: a.title, ArtistName: a.artist.name,
		ArtistURL: a.artist.url(), ArtworkURL: a.artwork(), ArtworkBgColor: a.bg, ArtworkTextColor1: a.fg,
		ArtworkTextColor2: "ffffff", ReleaseDate: a.releaseDate(), Genre: a.artist.genre, TrackCount: len(a.tracks),
		AudioTraits: a.traits()}
	if one != nil {
		l.Type, l.TrackID, l.Title, l.AlbumName, l.TrackCount, l.DurationMS = "song", one.id, a.tracks[one.index], a.title, 0, durationMS(one.id)
	}
	writeJSON(w, http.StatusOK, l)
}

func (b *Backend) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	term := strings.ToLower(strings.TrimSpace(q.Get("term")))
	if term == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "term is required")
		return
	}
	types := map[string]bool{"songs": true, "albums": true, "playlists": true, "artists": true}
	if t := q.Get("types"); t != "" {
		types = map[string]bool{}
		for _, s := range strings.Split(t, ",") {
			types[s] = true
		}
	}
	limit, offset := pageOf(q, 10)
	match := func(s ...string) bool {
		for _, v := range s {
			if strings.Contains(strings.ToLower(v), term) {
				return true
			}
		}
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	g := b.hits(match)
	if len(g.artists)+len(g.albums)+len(g.playlists)+len(g.songs) == 0 {
		// Visitors search for real artists, which the made-up catalog lacks:
		// a term that matches nothing answers with the whole catalog, as the
		// website's console demo does (apps/site/demo/fake-api.js).
		g = b.hits(func(...string) bool { return true })
	}
	res := client.SearchResult{Term: q.Get("term"), Storefront: "us"}
	if types["artists"] {
		res.Artists = page(g.artists, limit, offset)
	}
	if types["albums"] {
		res.Albums = page(g.albums, limit, offset)
	}
	if types["playlists"] {
		res.Playlists = page(g.playlists, limit, offset)
	}
	if types["songs"] {
		res.Songs = page(g.songs, limit, offset)
	}
	writeJSON(w, http.StatusOK, res)
}

type searchHits struct{ artists, albums, playlists, songs []client.SearchHit }

// hits is the catalog that match keeps, by type. The caller holds b.mu.
func (b *Backend) hits(match func(...string) bool) searchHits {
	var g searchHits
	for _, a := range artists {
		if match(a.name, a.genre) {
			g.artists = append(g.artists, a.hit())
		}
	}
	for _, a := range albums {
		if match(a.title, a.artist.name, a.artist.genre) {
			g.albums = append(g.albums, b.marked(a.hit()))
		}
	}
	for _, p := range playlists {
		if match(p.name, p.curator) {
			g.playlists = append(g.playlists, b.marked(p.hit()))
		}
	}
	for _, a := range albums {
		for i, name := range a.tracks {
			if match(name, a.title, a.artist.name) {
				g.songs = append(g.songs, b.marked(song{id: songID(a, i), album: a, index: i}.hit()))
			}
		}
	}
	return g
}

// marked gives a hit the job that holds it, as the library and queue marks
// of search read it.
func (b *Backend) marked(h client.SearchHit) client.SearchHit {
	if j, ok := b.priorJob(h.ID); ok {
		h.PriorJob = &client.PriorJob{ID: j.ID, Status: j.Status, FinishedAt: j.FinishedAt}
	}
	return h
}

func pageOf(q url.Values, def int) (limit, offset int) {
	limit, _ = strconv.Atoi(q.Get("limit"))
	offset, _ = strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = def
	}
	return min(limit, 50), max(offset, 0)
}

func page(hits []client.SearchHit, limit, offset int) *client.HitGroup {
	g := &client.HitGroup{Items: []client.SearchHit{}}
	if offset < len(hits) {
		end := min(offset+limit, len(hits))
		g.Items = hits[offset:end]
		if end < len(hits) {
			g.NextOffset = end
		}
	}
	return g
}

func (b *Backend) artistAlbums(w http.ResponseWriter, r *http.Request) {
	a := findArtist(r.PathValue("id"))
	if a == nil {
		writeError(w, http.StatusNotFound, "catalog_not_found", "no such artist")
		return
	}
	limit, offset := pageOf(r.URL.Query(), 25)
	b.mu.Lock()
	defer b.mu.Unlock()
	var hits []client.SearchHit
	for _, al := range a.albums() {
		hits = append(hits, b.marked(al.hit()))
	}
	writeJSON(w, http.StatusOK, client.ArtistAlbums{Storefront: "us", Artist: a.hit(), Albums: *page(hits, limit, offset)})
}

func (b *Backend) album(w http.ResponseWriter, r *http.Request) {
	a := findAlbum(r.PathValue("id"))
	if a == nil {
		writeError(w, http.StatusNotFound, "catalog_not_found", "no such album")
		return
	}
	writeJSON(w, http.StatusOK, a.detail())
}

// cover serves an album's cover at the size of its template, {w}x{h}bb.{f}.
func (b *Backend) cover(w http.ResponseWriter, r *http.Request) {
	a := findAlbum(r.PathValue("id"))
	var px int
	if _, err := fmt.Sscanf(r.PathValue("size"), "%dx", &px); a == nil || err != nil {
		http.NotFound(w, r)
		return
	}
	px = min(max(px, 16), coverMax)
	key := fmt.Sprintf("%s/%d", a.id, px)
	b.mu.Lock()
	img, ok := b.covers[key]
	b.mu.Unlock()
	if !ok {
		img = cover(a, px)
		b.mu.Lock()
		b.covers[key] = img
		b.mu.Unlock()
	}
	w.Header().Set("Content-Type", "image/png")
	// The client reads it in this process; a failed write means it stopped.
	_, _ = w.Write(img)
}
