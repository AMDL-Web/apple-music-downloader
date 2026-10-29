// Path templates (download.*_path_format): the variables
// configs/config.example.yaml documents, for the Settings page's Variables
// popover, its unknown-variable warning and the example path under each
// template. The example follows internal/media/paths.go (formatPattern,
// templateValue, safeName, outputPath) rule for rule. Pure.

/** The template variables config.example.yaml documents, with what each holds. */
export const TEMPLATE_VARIABLES = [
  ['SongName', 'Track title'],
  ['SongNumber', '1-based position in the collection it is downloaded as; keeps counting across discs'],
  ['TrackNumber', 'Apple’s track number, restarting at 1 on every disc'],
  ['TrackCount', 'Tracks on the track’s own disc'],
  ['DiscNumber', 'Disc number'],
  ['DiscCount', 'Number of discs'],
  ['SongId', 'Apple song id'],
  ['ArtistName', 'In folders the collection’s artist, in the file name the track’s artist'],
  ['UrlArtistName', 'Same as ArtistName'],
  ['ArtistId', 'Apple artist id'],
  ['AlbumName', 'Album title'],
  ['AlbumId', 'Apple album id'],
  ['AlbumArtist', 'Album artist'],
  ['ReleaseDate', 'Release date'],
  ['ReleaseYear', 'Release year'],
  ['UPC', 'Album UPC'],
  ['Copyright', 'Copyright line'],
  ['RecordLabel', 'Record label'],
  ['PlaylistName', 'Playlist title'],
  ['PlaylistId', 'Apple playlist id'],
  ['StationName', 'Station title'],
  ['StationId', 'Apple station id'],
  ['Quality', 'Quality label of the chosen codec'],
  ['Codec', 'Chosen codec'],
  ['Tag', 'Always empty; accepted so older templates still parse'],
];

/**
 * Variables that take :02d zero padding, e.g. {TrackNumber:02d}. The
 * backend reads only the exact suffix ":02d".
 */
export const PADDED_VARIABLES = ['SongNumber', 'DiscNumber', 'DiscCount', 'TrackNumber', 'TrackCount'];

const KNOWN_VARS = new Set(TEMPLATE_VARIABLES.map(([v]) => v));

/**
 * Variables in a path template the backend does not know (they stay in the
 * path literally), and padded variables that take no padding. Order of
 * appearance, unique.
 */
export function templateUnknownVars(template) {
  const out = [];
  for (const m of String(template || '').matchAll(/\{([^{}]*)\}/g)) {
    const [name, pad] = m[1].split(':');
    const bad = !KNOWN_VARS.has(name) || (pad !== undefined && (!PADDED_VARIABLES.includes(name) || pad !== '02d'));
    if (bad && !out.includes(m[0])) out.push(m[0]);
  }
  return out;
}

/** Which template a path_format key is: album, song, playlist, artist or station. */
export const templateKind = (key) => (/download\.([a-z]+)_path_format/.exec(key) || [])[1] || 'album';

// One sample track for the example under each template: track 1 of Blonde
// (ids and UPC are placeholders).
// Playlist and station names exist only for those downloads, as on the server.
const SAMPLE = {
  SongName: 'Nikes',
  SongId: '1000000001',
  ArtistName: 'Frank Ocean',
  UrlArtistName: 'Frank Ocean',
  ArtistId: '1000000002',
  AlbumName: 'Blonde',
  AlbumId: '1146195596',
  AlbumArtist: 'Frank Ocean',
  ReleaseDate: '2016-08-20',
  ReleaseYear: '2016',
  UPC: '0000000000000',
  Copyright: '℗ 2016 Boys Don’t Cry',
  RecordLabel: 'Boys Don’t Cry',
  DiscNumber: '1',
  DiscCount: '1',
  TrackNumber: '1',
  TrackCount: '17',
  Quality: '16-bit/44 kHz',
  Codec: 'ALAC',
  Tag: '',
};
const COLLECTIONS = {
  playlist: { PlaylistName: 'Summer Mix', PlaylistId: 'pl.u-sample' },
  station: { StationName: 'Frank Ocean Radio', StationId: 'ra.sample' },
};

// safeName: reserved characters become "_", dots and white space go from both
// ends, and an empty name is "_".
const safeName = (v) => v.replace(/[/\\<>:"|?*]/g, '_').replace(/^[.\s]+|[.\s]+$/gu, '') || '_';

/**
 * The relative path a template gives the sample track (".m4a" added), for
 * the line under the template field: "albums/Frank Ocean/Blonde/01.
 * Nikes.m4a". Unknown variables stay as written, as on the server.
 */
export function templateExample(template, kind = 'album') {
  const values = { ...SAMPLE, SongNumber: '1', PlaylistName: '', PlaylistId: '', StationName: '', StationId: '', ...(COLLECTIONS[kind] || {}) };
  const segments = String(template || '').split('/');
  return segments
    .map((segment, i) => {
      const formatted = segment.replace(/\{([A-Za-z]+)(?::(02d))?\}/g, (whole, name, pad) => {
        if (!Object.hasOwn(values, name)) return whole;
        const v = values[name];
        return pad ? String(Number.parseInt(v, 10) || 0).padStart(2, '0') : v;
      });
      return safeName(formatted) + (i === segments.length - 1 ? '.m4a' : '');
    })
    .join('/');
}
