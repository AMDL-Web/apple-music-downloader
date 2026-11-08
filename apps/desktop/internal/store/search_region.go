package store

import (
	"net/url"
	"slices"
	"strings"

	"amdl/internal/client"
)

// SearchRegions are the storefronts the logged-in accounts serve, the
// default account's first, each once. Search can read any of them; with
// fewer than two there is nothing to choose.
func (s *Store) SearchRegions() []string {
	var out []string
	for _, a := range s.Server.Accounts {
		r := strings.ToLower(a.Region)
		if a.State == client.AccountMissing || r == "" || slices.Contains(out, r) {
			continue
		}
		if a.Default {
			out = slices.Insert(out, 0, r)
		} else {
			out = append(out, r)
		}
	}
	return out
}

// SearchRegion is the storefront the search shows as chosen: the person's
// pick while an account serves it, else the default account's, "" when no
// account says.
func (s *Store) SearchRegion() string {
	regions := s.SearchRegions()
	if r := s.Search.Region; r != "" && slices.Contains(regions, r) {
		return r
	}
	if len(regions) > 0 {
		return regions[0]
	}
	return ""
}

// searchStorefront is what a catalog read sends: the person's pick while an
// account serves it, else "" and the backend's default.
func (s *Store) searchStorefront() string {
	if r := s.Search.Region; r != "" && slices.Contains(s.SearchRegions(), r) {
		return r
	}
	return ""
}

// searchKey keys an answer of the search or of an artist by the region it
// was read in, so a switch back finds its own hits.
func (s *Store) searchKey(k string) string { return s.SearchRegion() + "\x00" + k }

// SetSearchRegion searches region instead: the artist opened from the hits
// closes, and the current words are searched again at once.
func (s *Store) SetSearchRegion(region string) {
	q := &s.Search
	region = strings.ToLower(strings.TrimSpace(region))
	if !slices.Contains(s.SearchRegions(), region) {
		return
	}
	moved := region != s.SearchRegion()
	q.Region = region
	if !moved {
		return
	}
	q.gen++
	s.OpenArtist("")
	if q.want != "" && !s.showCachedSearch(q.want) {
		s.startSearch(q.want)
	}
}

// showCachedSearch shows the answer already read for term in the region
// chosen, and reports whether there was one.
func (s *Store) showCachedSearch(term string) bool {
	q := &s.Search
	r, ok := q.cache.get(s.searchKey(term))
	if ok {
		q.Term, q.Status, q.Result, q.Stale, q.Err, q.More = term, SearchReady, &r, false, "", nil
	}
	return ok
}

// albumKey keys an album by the storefront it is read in; "" is the
// backend's default.
func albumKey(storefront, id string) string { return storefront + "\x00" + id }

// albumKeyOf is the album id of an albumKey.
func albumKeyOf(key string) string {
	_, id, _ := strings.Cut(key, "\x00")
	return id
}

// LinkStorefront is the storefront an Apple Music link names, lower case;
// "" for anything else.
func LinkStorefront(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host != "music.apple.com" {
		return ""
	}
	sf, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	sf = strings.ToLower(sf)
	if len(sf) != 2 || sf[0] < 'a' || sf[0] > 'z' || sf[1] < 'a' || sf[1] > 'z' {
		return ""
	}
	return sf
}
