# applemusic test fixtures

amp-api responses the tests serve through `newTestClient` (helpers_test.go).

Trimmed live captures (amp-api answers recorded once, with `meta`, previews and
editorial notes removed; used by catalog_search_test.go):

- `search-us.json`: `GET /v1/catalog/us/search?term=Taylor Swift&types=songs,albums,playlists,artists&limit=2&l=zh-Hans_CN&include[songs]=albums`
- `search-jp.json`: `GET /v1/catalog/jp/search?term=星街すいせい&types=songs,artists&limit=2&offset=4`
- `search-playlists-us.json`: `GET /v1/catalog/us/search?term=lofi beats to study&types=playlists&limit=2`
  (one listener playlist, one Apple editorial playlist)
- `artist-albums-us.json`: `GET /v1/catalog/us/artists/159260351/albums?limit=3&offset=3`

Hand-written fixtures in `catalog/` (extracted from inline test strings; the ids
and names are made up, the shape follows amp-api):

- `song-1-in-album-1.json`, `album-1-two-tracks.json`: a song and the album it
  is enriched from (catalog_test.go)
- `album-paged.json`, `album-paged-tracks-offset-1.json`: an album whose track
  relationship continues on a `next` page, with music videos mixed in
- `album-artwork-colors.json`: the artwork palette keys on an album and a track
- `album-explicit-adm.json`, `album-plain.json`: content rating, Apple Digital
  Master and audio traits present and absent
- `artist-with-albums.json`, `artist-albums-offset-2.json`: an artist with an
  included album page, a `next` page and a duplicate across pages
- `playlist-paged.json`, `playlist-paged-tracks-offset-1.json`: a playlist
  whose tracks continue on a `next` page (catalog_playlist_test.go)
- `playlist-private-with-library.json`: a private playlist without a catalog
  cover and its library relationship's cover
- `station-next-tracks.json`: a station's next-tracks answer with a music video
  mixed in (catalog_station_test.go)
