// The Settings page model: one field per runtime config key
// (GET /api/v1/config, the RuntimeConfig schema), the PUT body builder and the
// mapping of API errors onto fields. Pure: no DOM, so node --test covers it.
//
// Every field's key is one full dotted string literal, never built from parts:
// TestSettingsFieldsCoverRuntimeConfig in console_test.go reads them and
// fails when RuntimeConfig gains a leaf this list neither edits nor names in
// NOT_EDITED, or when a field names a key the API does not return. Help text
// condenses the key's comment in configs/config.example.yaml.

// An install upgraded from a version whose album and artist templates used
// {TrackNumber} keeps that layout (configs/config.example.yaml explains).
const OLDER_LAYOUT_NOTE = 'An install upgraded from an older version may still use {TrackNumber:02d} instead.';

/**
 * One field per editable key. type: switch | select | number | text | path |
 * template | token | list (ordered, from options) | set (unordered, from
 * options). A `custom` select also takes any other value through a text box. `help` is one short line about the effect; `defaultValue` is the
 * value a key missing from the file takes (configs/config.example.yaml), shown
 * with the key on demand and used by Reset to default. Fields under a
 * section's sub-heading name their `group`. select options may carry a hint, added to the help line while chosen.
 */
export const FIELDS = [
  // Account
  {
    key: 'catalog.media_user_token',
    section: 'account',
    type: 'token',
    label: 'Media user token',
    help: 'Optional: stations, private playlist covers, library sync.',
    defaultValue: '',
  },
  // Quality
  {
    key: 'download.quality_priority',
    section: 'quality',
    type: 'list',
    label: 'Codec priority',
    help: 'Tried top to bottom · AAC always last',
    defaultValue: ['alac'],
    minItems: 1,
    addLabel: 'Add codec',
    addNoun: 'codec',
    options: [
      { value: 'alac', label: 'ALAC (lossless)', hint: 'Apple Lossless, up to the limits below' },
      { value: 'ec3', label: 'Dolby Atmos (E-AC-3)', hint: 'Dolby Atmos in Dolby Digital Plus' },
      { value: 'ac3', label: 'Dolby Digital (AC-3)', hint: 'Dolby Digital surround' },
      { value: 'aac', label: 'AAC (Enhanced HLS)', hint: 'The Enhanced HLS AAC stream' },
      { value: 'aac-binaural', label: 'AAC binaural', hint: 'Spatial audio rendered for headphones' },
      { value: 'aac-downmix', label: 'AAC Dolby downmix', hint: 'Spatial audio downmixed to stereo' },
    ],
  },
  {
    key: 'download.codec_alternative',
    section: 'quality',
    type: 'switch',
    label: 'Fall back to the next codec',
    help: 'When a codec fails, try the next one in the list.',
    defaultValue: true,
  },
  {
    key: 'download.alac_max_sample_rate',
    section: 'quality',
    type: 'select',
    label: 'Highest ALAC sample rate',
    help: 'Higher-rate ALAC versions are skipped.',
    defaultValue: 192000,
    options: [
      { value: 44100, label: '44.1 kHz' },
      { value: 48000, label: '48 kHz' },
      { value: 88200, label: '88.2 kHz' },
      { value: 96000, label: '96 kHz' },
      { value: 176400, label: '176.4 kHz' },
      { value: 192000, label: '192 kHz' },
    ],
  },
  {
    key: 'download.alac_max_bit_depth',
    section: 'quality',
    type: 'select',
    label: 'Highest ALAC bit depth',
    help: 'Deeper ALAC versions are skipped.',
    defaultValue: 24,
    options: [
      { value: 16, label: '16-bit' },
      { value: 24, label: '24-bit' },
    ],
  },
  {
    key: 'catalog.album_track_url_mode',
    section: 'quality',
    type: 'select',
    label: 'When an album link points to one track',
    help: 'For example a link copied from a song on an album page.',
    defaultValue: 'song',
    options: [
      { value: 'song', label: 'Download that track' },
      { value: 'album', label: 'Download the whole album' },
    ],
  },
  // Storage & files
  {
    key: 'download.downloads_dir',
    section: 'files',
    type: 'path',
    label: 'Downloads folder',
    help: 'Where finished tracks are saved. A relative path starts at the backend’s folder.',
    defaultValue: 'data/downloads',
  },
  {
    key: 'download.temp_dir',
    section: 'files',
    type: 'path',
    label: 'Temporary folder',
    help: 'Work space while a track is processed; on the same disk as the downloads folder is fastest.',
    defaultValue: 'data/tmp',
  },
  {
    key: 'download.force_overwrite',
    section: 'files',
    type: 'switch',
    label: 'Overwrite existing files',
    help: 'Download tracks again even when they are already on disk.',
    defaultValue: false,
  },
  { key: 'download.album_path_format', section: 'files', type: 'template', label: 'Albums', help: '{SongNumber} keeps counting across discs.', defaultValue: 'albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}', defaultNote: OLDER_LAYOUT_NOTE },
  { key: 'download.song_path_format', section: 'files', type: 'template', label: 'Songs', help: '{SongNumber} is always 1; {TrackNumber} keeps the album position.', defaultValue: 'songs/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}' },
  { key: 'download.playlist_path_format', section: 'files', type: 'template', label: 'Playlists', help: '{SongNumber} is the position in the playlist.', defaultValue: 'playlists/{PlaylistName}/{SongNumber:02d}. {SongName}' },
  { key: 'download.artist_path_format', section: 'files', type: 'template', label: 'Artists', help: '{SongNumber} restarts at 1 for each album.', defaultValue: 'artists/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}', defaultNote: OLDER_LAYOUT_NOTE },
  { key: 'download.station_path_format', section: 'files', type: 'template', label: 'Stations', help: '{SongNumber} is the position in the station’s track list.', defaultValue: 'stations/{StationName}/{SongNumber:02d}. {SongName}' },
  // Lyrics & artwork
  {
    key: 'download.cover_size',
    section: 'lyrics_artwork',
    type: 'select',
    label: 'Cover size',
    help: 'Smaller standard sizes are tried when Apple lacks this one.',
    defaultValue: '5000x5000',
    // The sizes config.example.yaml documents; Custom… takes any other.
    options: [
      { value: '5000x5000', label: '5000 × 5000 px' },
      { value: '3000x3000', label: '3000 × 3000 px' },
      { value: '1400x1400', label: '1400 × 1400 px' },
      { value: '600x600', label: '600 × 600 px' },
      { value: '300x300', label: '300 × 300 px' },
    ],
    custom: true,
    customPlaceholder: 'Width x height, as 2000x2000',
    // The backend's rule (internal/config Validate).
    pattern: '^[1-9][0-9]*x[1-9][0-9]*$',
    patternHint: 'Write it as width x height in whole pixels above 0, for example 3000x3000.',
  },
  {
    key: 'download.cover_format',
    section: 'lyrics_artwork',
    type: 'select',
    label: 'Cover format',
    help: 'The image type of covers.',
    defaultValue: 'jpg',
    options: [
      { value: 'jpg', label: 'JPEG (.jpg)' },
      { value: 'jpeg', label: 'JPEG (.jpeg)' },
      { value: 'png', label: 'PNG (.png)' },
    ],
  },
  { key: 'download.embed_cover', section: 'lyrics_artwork', type: 'switch', label: 'Embed the cover in each track', help: 'Music players show it as the artwork.', defaultValue: true },
  { key: 'download.save_album_cover', section: 'lyrics_artwork', type: 'switch', label: 'Save an album cover file', help: 'Saved in the album’s folder.', defaultValue: true },
  { key: 'download.save_artist_cover', section: 'lyrics_artwork', type: 'switch', label: 'Save an artist picture file', help: 'Saved in the artist’s folder, when the path has one.', defaultValue: true },
  { key: 'download.save_playlist_cover', section: 'lyrics_artwork', type: 'switch', label: 'Save a playlist cover file', help: 'Saved with playlist downloads.', defaultValue: true },
  { key: 'download.embed_lyrics', section: 'lyrics_artwork', type: 'switch', label: 'Embed lyrics in each track', help: 'Synced LRC that music players show while the song plays.', defaultValue: true },
  {
    key: 'download.lyrics_files',
    section: 'lyrics_artwork',
    type: 'set',
    label: 'Lyrics files next to each track',
    help: 'LRC suits most players; TTML keeps Apple’s word timing, translation and romanization, which Navidrome 0.63+ reads.',
    defaultValue: [],
    options: [
      { value: 'lrc', label: 'LRC' },
      { value: 'ttml', label: 'TTML' },
    ],
  },
  {
    key: 'download.lyrics_timing',
    section: 'lyrics_artwork',
    type: 'select',
    label: 'Lyrics timing',
    help: 'How finely LRC lyrics follow the song.',
    defaultValue: 'line',
    options: [
      { value: 'line', label: 'Line by line' },
      { value: 'word', label: 'Word by word', hint: 'Enhanced LRC. Songs Apple doesn’t time word by word stay line by line.' },
    ],
  },
  {
    key: 'download.lyrics_extras',
    section: 'lyrics_artwork',
    type: 'set',
    label: 'Extra lines in LRC',
    help: 'Added under each LRC line when Apple has them; the translation is in the catalog language.',
    defaultValue: [],
    options: [
      { value: 'translation', label: 'Translation' },
      { value: 'pronunciation', label: 'Romanization' },
    ],
  },
  // Jobs
  {
    key: 'download.max_attempts',
    section: 'jobs',
    type: 'number',
    label: 'Tries per file',
    help: 'How many times each step is tried before the track fails (1-10).',
    defaultValue: 3,
    min: 1,
    max: 10,
  },
  {
    key: 'download.memory_mode',
    section: 'jobs',
    type: 'select',
    label: 'Memory use',
    help: 'Faster with more RAM, or slower and lighter.',
    defaultValue: 'low',
    options: [
      { value: 'low', label: 'Low memory', hint: 'Streams each track to disk: little RAM, more temporary files.' },
      { value: 'high', label: 'High memory', hint: 'Keeps one track in RAM, up to 512 MiB each.' },
    ],
  },
  {
    key: 'download.progress_event_interval_ms',
    section: 'jobs',
    type: 'number',
    label: 'Progress updates every',
    help: 'Shorter is smoother, longer is lighter; 0 updates each percent (0-10000).',
    defaultValue: 500,
    min: 0,
    max: 10000,
    unit: 'ms',
  },
  {
    key: 'download.check_integrity',
    section: 'jobs',
    type: 'switch',
    label: 'Check file integrity',
    help: 'Verify each finished file and repair known ALAC issues.',
    defaultValue: true,
  },
  {
    key: 'catalog.motion_artwork_enabled',
    section: 'lyrics_artwork',
    type: 'switch',
    label: 'Look up animated covers',
    help: 'One extra request per album; it never holds up a download.',
    defaultValue: true,
  },
  // Library sync
  {
    key: 'library_sync.enabled',
    section: 'library_sync',
    type: 'switch',
    label: 'Watch my library',
    help: 'Needs the media user token.',
    defaultValue: false,
  },
  {
    key: 'library_sync.interval_minutes',
    section: 'library_sync',
    type: 'number',
    label: 'Check every',
    help: 'A check is usually one Apple request (1-1440).',
    defaultValue: 15,
    min: 1,
    max: 1440,
    unit: 'min',
  },
  // Logs
  {
    key: 'logging.level',
    section: 'logging',
    type: 'select',
    label: 'Log level',
    help: 'Messages below this level are dropped.',
    defaultValue: 'info',
    options: [
      { value: 'debug', label: 'Debug' },
      { value: 'info', label: 'Info' },
      { value: 'warn', label: 'Warn' },
      { value: 'error', label: 'Error' },
    ],
  },
  { key: 'logging.access_log', section: 'logging', type: 'switch', label: 'HTTP access log', help: 'One line per HTTP request.', defaultValue: false },
  // Server › Simulate
  {
    key: 'simulate.enabled',
    section: 'server',
    group: 'simulate',
    type: 'switch',
    label: 'Simulate mode',
    help: 'Jobs move through their steps at the simulated speeds below.',
    defaultValue: false,
  },
  { key: 'simulate.min_speed_kbps', section: 'server', group: 'simulate', type: 'number', label: 'Slowest simulated speed', help: 'At least 1.', defaultValue: 512, min: 1, unit: 'KB/s' },
  { key: 'simulate.max_speed_kbps', section: 'server', group: 'simulate', type: 'number', label: 'Fastest simulated speed', help: 'At least the slowest speed.', defaultValue: 4096, min: 1, unit: 'KB/s' },
];

/**
 * RuntimeConfig keys the page shows no field for, each with the reason.
 * Empty: every runtime key is editable here.
 */
export const NOT_EDITED = {};

export const FIELD_KEYS = FIELDS.map((f) => f.key);
export const BY_KEY = new Map(FIELDS.map((f) => [f.key, f]));

export const fieldByKey = (key) => BY_KEY.get(key);

const has = (list, v) => Array.isArray(list) && list.includes(v);
// Fields that apply only while another value allows them: shown disabled,
// never hidden, so the section keeps its height when a switch flips.
const REQUIRES = {
  'download.alac_max_sample_rate': (v) => has(v('download.quality_priority'), 'alac'),
  'download.alac_max_bit_depth': (v) => has(v('download.quality_priority'), 'alac'),
  'download.lyrics_timing': (v) => v('download.embed_lyrics') === true || has(v('download.lyrics_files'), 'lrc'),
  'download.lyrics_extras': (v) => v('download.embed_lyrics') === true || has(v('download.lyrics_files'), 'lrc'),
  'simulate.min_speed_kbps': (v) => v('simulate.enabled') === true,
  'simulate.max_speed_kbps': (v) => v('simulate.enabled') === true,
};

/** Whether the field of `key` takes no input under the values `valueOf` gives (the draft over the base). */
export const fieldDisabled = (key, valueOf) => Object.hasOwn(REQUIRES, key) && !REQUIRES[key](valueOf);
export const sectionFields = (id) => FIELDS.filter((f) => f.section === id);

/** A value as the page words it: On/Off, an option's label, a list of labels, a number with its unit. */
export function valueLabel(field, v) {
  const label = (x) => (field.options.find((o) => o.value === x) || { label: String(x) }).label;
  switch (field.type) {
    case 'switch':
      return v ? 'On' : 'Off';
    case 'select':
      return label(v);
    case 'list':
    case 'set':
      return Array.isArray(v) && v.length ? v.map(label).join(', ') : 'None';
    case 'number':
      return field.unit ? `${v} ${field.unit}` : String(v);
    default:
      return v === '' ? 'Empty' : String(v);
  }
}
