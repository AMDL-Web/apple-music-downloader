// The mock server's catalog search and an artist's albums, from the fixed
// catalog in fixtures.mjs.

import { CATALOG, PLAYLISTS } from './fixtures.mjs';
import { json } from './mock-http.mjs';
import { list } from './mock-world.mjs';

// A term's hits: the recorded ones first, then the rest of the catalog,
// so albums, songs and playlists run to several pages. Playlists come
// from the term's artists; a term with none of them ("beethoven") has
// none, and "zzzz" finds nothing at all.
export function search(world, res, q) {
  const term = (q.get('term') || '').toLowerCase().trim();
  const types = list(q.get('types'));
  const limit = Number(q.get('limit')) || 10;
  const offset = Number(q.get('offset')) || 0;
  const hit = Object.entries(CATALOG.searches).find(([k]) => k.includes(term) || term.includes(k)) || Object.entries(CATALOG.searches)[0];
  const byId = (arr, ids) => ids.map((id) => arr.find((x) => x.id === id)).filter(Boolean);
  const padded = (arr, ids) => {
    const first = byId(arr, ids);
    const seen = new Set(first.map((x) => x.id));
    return [...first, ...arr.filter((x) => !seen.has(x.id))];
  };
  const artists = byId(CATALOG.artists, hit[1].artists);
  const names = new Set(artists.map((a) => a.name));
  const own = PLAYLISTS.filter((p) => [...names].some((n) => p.name.startsWith(`${n} `)));
  const groups =
    term === 'zzzz'
      ? { songs: [], albums: [], playlists: [], artists: [] }
      : {
          songs: padded(CATALOG.songs, hit[1].songs),
          albums: padded(CATALOG.albums, hit[1].albums),
          playlists: own.length ? [...own, ...PLAYLISTS.filter((p) => !own.includes(p))] : [],
          artists,
        };
  // prior_job as the server sets it: the newest active job of the hit's
  // type and id, else the newest completed one.
  const jobs = [...world.jobs.values()].sort((x, y) => y.created_at.localeCompare(x.created_at));
  const withPrior = (h) => {
    const mine = jobs.filter((j) => j.type === h.type && j.input.endsWith(`/${h.id}`));
    const j = mine.find((x) => x.status === 'queued' || x.status === 'running') || mine.find((x) => x.status === 'completed');
    return j ? { ...h, prior_job: { id: j.id, status: j.status, ...(j.finished_at ? { finished_at: j.finished_at } : {}) } } : h;
  };
  const out = { term: q.get('term'), storefront: 'cn' };
  for (const [g, arr] of Object.entries(groups)) {
    if (types.length && !types.includes(g)) continue;
    out[g] = { items: arr.slice(offset, offset + limit).map(withPrior), ...(offset + limit < arr.length ? { next_offset: offset + limit } : {}) };
  }
  return json(res, 200, out);
}

export function artistAlbums(res, id, q) {
  const artist = CATALOG.artists.find((a) => a.id === id) || CATALOG.artists[0];
  const albums = CATALOG.albums.filter((a) => a.artist_name === artist.name);
  const offset = Number(q.get('offset')) || 0;
  const limit = Number(q.get('limit')) || 25;
  const page = albums.slice(offset, offset + limit);
  return json(res, 200, { storefront: 'cn', artist, albums: { items: page, ...(offset + limit < albums.length ? { next_offset: offset + limit } : {}) } });
}
