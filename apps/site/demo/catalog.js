// The demo's catalog: made-up artists, albums, playlists and songs, with
// covers drawn as SVG color fields. No name here belongs to a real release,
// and no URL is ever fetched: the music.apple.com links only give the
// console's link parser the shape it expects.

const STOREFRONT = 'us';

const slug = (s) => encodeURIComponent(s.toLowerCase().replace(/[\s/]+/g, '-').replace(/[^\p{L}\p{N}-]/gu, '').replace(/-+/g, '-'));

const hex = (h) => [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
const toHex = (rgb) => rgb.map((v) => Math.round(v).toString(16).padStart(2, '0')).join('');
const mix = (a, b, t) => toHex(hex(a).map((v, i) => v + (hex(b)[i] - v) * t));
const luminance = (h) => {
  const [r, g, b] = hex(h).map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};

// One abstract motif per cover, drawn over a two-color gradient in a
// 100-unit square.
const MOTIFS = {
  sun: (a, b) => `<circle cx="50" cy="62" r="26" fill="#${b}" opacity=".85"/><path d="M0 70h100M0 78h100M0 86h100M0 94h100" stroke="#${a}" stroke-width="3"/>`,
  rings: (a, b) => `<g fill="none" stroke="#${b}" stroke-width="2.5" opacity=".75"><circle cx="64" cy="40" r="10"/><circle cx="64" cy="40" r="20"/><circle cx="64" cy="40" r="30"/><circle cx="64" cy="40" r="40"/></g>`,
  bars: (a, b) => `<g fill="#${b}" opacity=".85">${[34, 58, 46, 72, 40, 64, 28, 52]
    .map((h, i) => `<rect x="${10 + i * 10.5}" y="${88 - h}" width="6" height="${h}" rx="3"/>`)
    .join('')}</g>`,
  grid: (a, b) => `<g fill="#${b}" opacity=".8">${Array.from({ length: 25 }, (_, i) => `<circle cx="${18 + (i % 5) * 16}" cy="${18 + Math.floor(i / 5) * 16}" r="${2 + ((i * 7) % 5) * 0.9}"/>`).join('')}</g>`,
  wave: (a, b) => `<g fill="none" stroke="#${b}" stroke-width="3" opacity=".8">${[38, 52, 66, 80]
    .map((y) => `<path d="M-5 ${y}q13-10 26 0t26 0 26 0 26 0"/>`)
    .join('')}</g>`,
  split: (a, b) => `<path d="M0 100L100 0v100z" fill="#${b}" opacity=".55"/><circle cx="30" cy="30" r="9" fill="#${b}"/>`,
};

/** A cover as a data: URL, plus the five palette keys Apple sends with artwork. */
export function cover([a, b], motif) {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#${a}"/><stop offset="1" stop-color="#${mix(a, b, 0.45)}"/></linearGradient></defs><rect width="100" height="100" fill="url(#g)"/>${MOTIFS[motif](a, b)}</svg>`;
  const ink = luminance(a) < 0.35 ? 'ffffff' : '000000';
  return {
    artwork_url: `data:image/svg+xml,${encodeURIComponent(svg)}`,
    artwork_bg_color: a,
    artwork_text_color1: mix(b, ink, 0.85),
    artwork_text_color2: mix(b, ink, 0.55),
    artwork_text_color3: mix(b, ink, 0.7),
    artwork_text_color4: mix(b, ink, 0.45),
  };
}

const ARTISTS = [
  ['Saffron Tide', 'R&B/Soul', ['5b1d3a', 'e8a25c'], 'sun'],
  ['Northbound Static', 'Electronic', ['0f3b5f', '2dd4bf'], 'bars'],
  ['Mira Lune', 'Dream Pop', ['3b1d6e', 'f0abfc'], 'rings'],
  ['Hollow Pines', 'Folk', ['2f3d16', 'd9f99d'], 'wave'],
  ['雾港', '华语流行', ['0b4f63', 'a5f3fc'], 'wave'],
  ['風町あおい', 'J-Pop', ['fce7f3', 'db2777'], 'sun'],
  ['Orchestra of the Long Afternoon', 'Classical', ['172554', '93c5fd'], 'split'],
  ['Lumen & Lark', 'Singer/Songwriter', ['fef3c7', 'b45309'], 'grid'],
  ['Glass Orchard', 'Indie Rock', ['7f1d1d', 'fca5a5'], 'rings'],
  ['Coral Arcade', 'Synth-Pop', ['ff6b6b', 'fde047'], 'bars'],
  ['Juniper Avenue', 'Alternative', ['18181b', 'facc15'], 'bars'],
  ['The Quiet Engines', 'Post-Rock', ['1f2937', 'f59e0b'], 'split'],
];

const NOCTURNES = ['A Minor: Lento', 'F Major: Andante', 'D Minor: Adagio', 'B-Flat Major: Larghetto', 'E Minor: Lento assai', 'G Major: Andantino', 'C-Sharp Minor: Grave', 'A-Flat Major: Moderato', 'F Minor: Adagio sostenuto', 'E-Flat Major: Andante cantabile', 'B Minor: Lento', 'D Major: Allegretto', 'G Minor: Largo', 'C Major: Andante tranquillo'];

// [artist index, title, release date, colors, motif, audio traits, track titles]
const ALBUMS = [
  [0, 'Velvet Hours', '2026-03-13', ['5b1d3a', 'e8a25c'], 'sun', 'atmos', ['Velvet Hours', 'Honey on the Radio', 'Slow Bloom', 'Amber Streetlights', 'Call Me Back Tomorrow', 'Sundays in Blue', 'Gold Thread', 'Warm Static', 'Linger', 'Paper Moon Motel', 'Afterglow (Interlude)', 'Halfway Home', 'The Last Slow Song']],
  [1, 'Signal / Noise', '2025-11-07', ['0f3b5f', '2dd4bf'], 'bars', 'atmos', ['Carrier Wave', 'Signal / Noise', 'Night Bus', 'Low Orbit', 'Static Bloom', 'Platform 9', 'Neon Arithmetic', 'Overpass', 'Long Exposure', 'Dawn Protocol']],
  [2, 'Satellite Garden', '2026-06-19', ['3b1d6e', 'f0abfc'], 'rings', 'atmos', ['Satellite Garden', 'Moth Light', 'Glasshouse', 'Parachute Season', 'Orbiting You', 'Soft Machinery', 'Silver Lake Swim', 'Static Lullaby', 'Fern & Fog', 'Overgrown', 'Goodnight, Telescope']],
  [3, 'Cedar Hours - EP', '2026-09-05', ['2f3d16', 'd9f99d'], 'wave', 'lossless', ['Cedar Hours', 'Woodsmoke', 'The River Knows', 'Kindling', 'Home by Lantern Light']],
  [4, '潮汐来信', '2026-05-20', ['0b4f63', 'a5f3fc'], 'wave', 'lossless', ['潮汐来信', '雾港', '末班渡轮', '灯塔守夜人', '盐', '退潮以后', '海风收音机', '晚安，礁石', '长堤', '回航']],
  [5, 'ひかりの地図', '2026-02-14', ['fce7f3', 'db2777'], 'sun', 'hires', ['ひかりの地図', '放課後スピカ', '風の通り道', 'シャボン', '夜明けのメロディ', 'ガラスの街', '雨上がりサイダー', 'ひとりぼっちの惑星', '羅針盤', 'まばたき', 'スターライト・バス', 'またね']],
  [6, 'Studies in Blue: Nocturnes for Strings', '2025-09-26', ['172554', '93c5fd'], 'split', 'hires', NOCTURNES.map((k, i) => `Nocturne No. ${i + 1} in ${k}`)],
  [2, 'Soft Machinery - Single', '2026-04-03', ['c4b5fd', '4c1d95'], 'rings', 'atmos', ['Soft Machinery']],
  [7, 'Paper Boats', '2024-04-26', ['fef3c7', 'b45309'], 'grid', 'lossless', ['Paper Boats', 'Kettle Song', 'Two Chairs', 'Morning Glass', 'Little Lighthouse', 'Thread & Needle', 'Postcards', 'Wild Thyme', "Sleepwalker's Waltz", 'Harbour Bells']],
  [8, 'Orchard Light', '2025-08-15', ['7f1d1d', 'fca5a5'], 'rings', 'lossless', ['Orchard Light', 'Bruised Fruit', 'Ladders', 'Cider Summer', 'Rows', 'Windfall', 'Bees in the Wall', 'Grafting', 'Harvest Moon Radio']],
  [9, 'High Score Summer', '2026-07-04', ['ff6b6b', 'fde047'], 'bars', 'atmos', ['High Score Summer', 'Insert Coin', 'Pixel Heart', 'Bonus Round', 'Coral Arcade', 'Continue?', 'Neon Boardwalk', 'Joystick Romance', 'Save Point', 'Game Over (Not Really)', 'Credits Roll']],
  [10, 'Late Trains', '2025-12-12', ['18181b', 'facc15'], 'bars', 'lossless', ['Late Trains', 'Ticket Stub', 'Junction', 'Waiting Room', 'Window Seat', 'Last Stop', 'Signal Failure', 'Rail Bridge', 'Northern Line', 'Terminus']],
  [3, 'Lanterns for the Long Walk', '2024-10-18', ['78350f', 'fbbf24'], 'sun', 'lossless', ['Lanterns', 'Long Walk', 'Pine Needle Bed', 'Snowmelt', 'Ridge Line', 'Old Map', 'Campfire Hymn', 'North Star', 'Footbridge', 'Hollow', 'Valley Echo', 'Homeward']],
  [11, 'Every Bridge Is a Promise', '2026-01-23', ['1f2937', 'f59e0b'], 'split', 'hires', ['Ignition', 'Every Bridge Is a Promise', 'Slow Cathedral', 'Rust & Rain', 'Undertow', 'The Long Quiet', 'Machines Dream Too', 'Engine Hymn']],
  [1, 'Night Bus Frequencies', '2024-06-07', ['1e1b4b', '818cf8'], 'grid', 'lossless', ['Last Departure', 'Sodium Lights', 'Route 40', 'Fogged Windows', 'Request Stop', 'Depot', 'Sleeper Service', 'Terminal Glow', 'First Light Home']],
  [4, '夏夜无声', '2024-08-08', ['134e4a', '99f6e4'], 'rings', 'lossless', ['夏夜无声', '萤火', '蝉鸣以后', '旧风扇', '晚风签名', '天台', '凌晨三点的海', '无声']],
  [5, 'まばたき - Single', '2026-10-02', ['fdf2f8', 'be185d'], 'grid', 'hires', ['まばたき']],
  [0, 'Golden Hour Sessions - EP', '2025-05-30', ['7c2d12', 'fdba74'], 'sun', 'lossless', ['Golden Hour (Live)', 'Slow Bloom (Acoustic)', 'Linger (Live)', 'Honey on the Radio (Stripped)']],
];

const TRAITS = {
  lossless: ['lossless', 'lossy-stereo'],
  hires: ['hi-res-lossless', 'lossless', 'lossy-stereo'],
  atmos: ['atmos', 'lossless', 'lossy-stereo', 'spatial'],
};

// Stable track lengths, 2:31 to 5:36, from the song's index.
const length = (n) => 151000 + ((n * 7919) % 185000);

export const artists = ARTISTS.map(([name, genre, colors, motif], i) => {
  const id = String(9300000001 + i);
  return { id, type: 'artist', name, url: `https://music.apple.com/${STOREFRONT}/artist/${slug(name)}/${id}`, ...cover(colors, motif), genre };
});

export const albums = [];
export const songs = [];
ALBUMS.forEach(([ai, name, date, colors, motif, traits, titles], i) => {
  const artist = ARTISTS[ai][0];
  const id = String(9100000001 + i);
  const art = cover(colors, motif);
  const album = {
    id,
    type: 'album',
    name,
    url: `https://music.apple.com/${STOREFRONT}/album/${slug(name)}/${id}`,
    ...art,
    artist_name: artist,
    artist_id: artists[ai].id,
    release_date: date,
    genre: ARTISTS[ai][1],
    track_count: titles.length,
    audio_traits: TRAITS[traits],
    quality: traits,
    tracks: [],
  };
  titles.forEach((title, t) => {
    const sid = String(9200000001 + songs.length);
    const song = {
      id: sid,
      type: 'song',
      name: title,
      url: `https://music.apple.com/${STOREFRONT}/song/${slug(title)}/${sid}`,
      ...art,
      artist_name: artist,
      album_name: name,
      album_id: id,
      release_date: date,
      genre: album.genre,
      duration_ms: length(songs.length + 1),
      audio_traits: album.audio_traits,
      track_number: t + 1,
    };
    songs.push(song);
    album.tracks.push(song);
  });
  albums.push(album);
});

/** Playlists are mixes of the songs above. */
export const playlists = [
  ['pl.d3m0a1b2c3d4e5f60718293a4b5c6d7e', 'Late Night Desk', ['1e1b4b', 'a5b4fc'], 'grid', (s, i) => i % 3 === 0, 12],
  ['pl.d3m0f9e8d7c6b5a40312a9b8c7d6e5f4', 'Rainy Window', ['334155', 'cbd5e1'], 'wave', (s, i) => i % 2 === 1, 28],
].map(([id, name, colors, motif, pick, n]) => ({
  id,
  type: 'playlist',
  name,
  url: `https://music.apple.com/${STOREFRONT}/playlist/${slug(name)}/${id}`,
  ...cover(colors, motif),
  curator_name: 'AMDL Demo',
  track_count: n,
  tracks: songs.filter(pick).slice(0, n),
}));

const byId = new Map([...albums, ...songs, ...artists, ...playlists].map((x) => [x.id, x]));

/** Any catalog entry by its id. */
export const find = (id) => byId.get(String(id));

export const albumNamed = (name) => albums.find((a) => a.name === name);
export const playlistNamed = (name) => playlists.find((p) => p.name === name);

/** A stable album for an id the catalog does not know, such as a pasted real link. */
export const albumFor = (id) => albums[[...String(id)].reduce((n, c) => n + c.charCodeAt(0), 0) % albums.length];

export { STOREFRONT };
