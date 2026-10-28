// Path templates (download.*_path_format): the variables
// configs/config.example.yaml documents, for the Settings page's reference
// list and its unknown-variable warning. Pure.

/** The template variables config.example.yaml documents, with what each holds. */
export const TEMPLATE_VARIABLES = [
  ['SongName', 'Track title'],
  ['SongNumber', '1-based position in the collection it is downloaded as; counts on across discs'],
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

/** Variables that take :02d style zero padding, e.g. {TrackNumber:02d}. */
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
    const bad = !KNOWN_VARS.has(name) || (pad !== undefined && (!PADDED_VARIABLES.includes(name) || !/^0\d+d$/.test(pad)));
    if (bad && !out.includes(m[0])) out.push(m[0]);
  }
  return out;
}
