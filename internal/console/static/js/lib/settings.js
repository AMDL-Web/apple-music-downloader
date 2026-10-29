// The Settings page model: its sections, one field per runtime config key
// (GET /api/v1/config, the RuntimeConfig schema), the PUT body builder and the
// mapping of API errors onto fields. Pure: no DOM, so node --test covers it.
//
// Every field's key is one full dotted string literal, never built from parts:
// TestSettingsFieldsCoverRuntimeConfig in console_test.go reads them and
// fails when RuntimeConfig gains a leaf this list neither edits nor names in
// NOT_EDITED, or when a field names a key the API does not return. Help text
// condenses the key's comment in configs/config.example.yaml.

/** The page's sections in nav order; `id` is the ?section= value. */
export const SETTINGS_SECTIONS = [
  { id: 'downloads', label: 'Downloads', icon: 'download', intro: 'Codecs, retries and what happens to files already on disk.' },
  { id: 'files', label: 'Files and paths', icon: 'hard-drive', intro: 'Where finished tracks go and how their folders and names are built.' },
  { id: 'artwork', label: 'Artwork', icon: 'disc', intro: 'Cover size and format, embedded and saved next to the tracks.' },
  { id: 'lyrics', label: 'Lyrics', icon: 'file-text', intro: 'Embedded lyrics and lyrics files.' },
  { id: 'catalog', label: 'Catalog', icon: 'music', intro: 'How links are read and which Apple requests run.' },
  { id: 'library_sync', label: 'Library sync', icon: 'rotate', intro: 'Download the album of every song you add to your Apple Music library.' },
  { id: 'logging', label: 'Logging', icon: 'list', intro: 'What the backend logs.' },
  { id: 'simulate', label: 'Simulate', icon: 'play', intro: 'Run jobs without downloading or writing files.' },
  { id: 'account', label: 'Apple Music account', icon: 'key', intro: 'The Apple Music accounts used to decrypt downloads. They live in wrapper-manager, a separate session service this backend connects to.' },
  { id: 'config_file', label: 'Configuration file', icon: 'braces', intro: 'Where these settings are stored, and the keys this page cannot change.' },
];

/** The section a ?section= value names, or the first one. */
export function sectionById(id) {
  return SETTINGS_SECTIONS.find((s) => s.id === id) || SETTINGS_SECTIONS[0];
}

// An install upgraded from a version whose album and artist templates used
// {TrackNumber} keeps that layout (configs/config.example.yaml explains).
const OLDER_LAYOUT_NOTE = 'An install upgraded from an older version may still use {TrackNumber:02d} instead.';

/**
 * One field per editable key. type: switch | select | number | text | path |
 * template | secret | list (ordered, from options) | set (unordered, from
 * options). select options may carry a hint shown under the control.
 */
export const FIELDS = [
  // Downloads
  {
    key: 'download.quality_priority',
    section: 'downloads',
    type: 'list',
    label: 'Codec priority',
    help: 'Tried top to bottom. AAC-LC is always the last fallback and needs no entry. Default: ALAC only.',
    minItems: 1,
    minItemsNote: 'At least one codec is required, so the last one cannot be removed. Add another first to swap it.',
    options: [
      { value: 'alac', label: 'ALAC (lossless)' },
      { value: 'ec3', label: 'Dolby Atmos (E-AC-3)' },
      { value: 'ac3', label: 'Dolby Digital (AC-3)' },
      { value: 'aac', label: 'AAC (Enhanced HLS)' },
      { value: 'aac-binaural', label: 'AAC binaural' },
      { value: 'aac-downmix', label: 'AAC Dolby downmix' },
    ],
  },
  {
    key: 'download.codec_alternative',
    section: 'downloads',
    type: 'switch',
    label: 'Fall back to the next codec',
    help: 'On: when a codec fails, the next one in the list is tried. Off: only the first. Default: on.',
  },
  {
    key: 'download.alac_max_sample_rate',
    section: 'downloads',
    type: 'select',
    label: 'Highest ALAC sample rate',
    help: 'ALAC versions above this rate are skipped. Default: 192 kHz.',
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
    section: 'downloads',
    type: 'select',
    label: 'Highest ALAC bit depth',
    help: 'ALAC versions deeper than this are skipped. Default: 24-bit.',
    options: [
      { value: 16, label: '16-bit' },
      { value: 24, label: '24-bit' },
    ],
  },
  {
    key: 'download.memory_mode',
    section: 'downloads',
    type: 'select',
    label: 'Memory mode',
    help: 'The RAM and disk trade-off for Enhanced HLS tracks; a submission may choose another. Default: low.',
    options: [
      { value: 'low', label: 'Low memory', hint: 'Streams each track to a resumable file: little RAM, about two track-sized files per parallel track in the temporary folder.' },
      { value: 'high', label: 'High memory', hint: 'Keeps one encrypted track in RAM (at most 512 MiB): about one track-sized file per parallel track in the temporary folder.' },
    ],
  },
  {
    key: 'download.max_attempts',
    section: 'downloads',
    type: 'number',
    label: 'Attempts per step',
    help: 'Total tries, the first included, for metadata, cover, lyrics and each codec. 1 to 10. Default: 3.',
    min: 1,
    max: 10,
  },
  {
    key: 'download.progress_event_interval_ms',
    section: 'downloads',
    type: 'number',
    label: 'Progress update interval',
    help: 'Least time between two progress events for one track; 0 leaves one per whole percent. Applies to jobs started later. 0 to 10000. Default: 500.',
    min: 0,
    max: 10000,
    unit: 'ms',
  },
  {
    key: 'download.check_integrity',
    section: 'downloads',
    type: 'switch',
    label: 'Check file integrity',
    help: 'Verifies each finished file and repairs known ALAC issues. Default: on.',
  },
  {
    key: 'download.force_overwrite',
    section: 'downloads',
    type: 'switch',
    label: 'Overwrite existing files',
    help: 'On: a track downloaded again replaces its file and lyrics file. Off: tracks already on disk are skipped. A submission may choose otherwise. Default: off.',
  },
  // Files and paths
  {
    key: 'download.downloads_dir',
    section: 'files',
    type: 'path',
    label: 'Downloads folder',
    help: 'The library root. A relative path starts at the backend working directory.',
    defaultValue: 'data/downloads',
  },
  {
    key: 'download.temp_dir',
    section: 'files',
    type: 'path',
    label: 'Temporary folder',
    help: 'Scratch space while a track is processed; only finished files move to the downloads folder. Fast local storage helps, and the same disk as the downloads folder makes the move a rename.',
    defaultValue: 'data/tmp',
  },
  { key: 'download.album_path_format', section: 'files', type: 'template', label: 'Albums', help: 'Album tasks. {SongNumber} counts on across discs.', defaultValue: 'albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}', defaultNote: OLDER_LAYOUT_NOTE },
  { key: 'download.song_path_format', section: 'files', type: 'template', label: 'Songs', help: 'Single-song tasks. {SongNumber} is always 1 here; {TrackNumber} keeps the album position.', defaultValue: 'songs/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}' },
  { key: 'download.playlist_path_format', section: 'files', type: 'template', label: 'Playlists', help: 'Playlist tasks. {SongNumber} is the position in the playlist.', defaultValue: 'playlists/{PlaylistName}/{SongNumber:02d}. {SongName}' },
  { key: 'download.artist_path_format', section: 'files', type: 'template', label: 'Artists', help: 'Artist tasks. {SongNumber} restarts at 1 for each album.', defaultValue: 'artists/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}', defaultNote: OLDER_LAYOUT_NOTE },
  { key: 'download.station_path_format', section: 'files', type: 'template', label: 'Stations', help: 'Radio station tasks. {SongNumber} is the position in the station’s track list.', defaultValue: 'stations/{StationName}/{SongNumber:02d}. {SongName}' },
  // Artwork
  {
    key: 'download.cover_size',
    section: 'artwork',
    type: 'text',
    label: 'Cover size',
    help: 'Width x height in pixels, for example 5000x5000, 3000x3000 or 1400x1400. Default: 5000x5000.',
    // The backend's rule (internal/config Validate).
    pattern: '^[1-9][0-9]*x[1-9][0-9]*$',
    patternHint: 'Write it as width x height in whole pixels above 0, for example 3000x3000.',
  },
  {
    key: 'download.cover_format',
    section: 'artwork',
    type: 'select',
    label: 'Cover format',
    help: 'Default: JPEG (.jpg).',
    options: [
      { value: 'jpg', label: 'JPEG (.jpg)' },
      { value: 'jpeg', label: 'JPEG (.jpeg)' },
      { value: 'png', label: 'PNG (.png)' },
    ],
  },
  { key: 'download.embed_cover', section: 'artwork', type: 'switch', label: 'Embed the cover in each track', help: 'Default: on.' },
  {
    key: 'download.save_album_cover',
    section: 'artwork',
    type: 'switch',
    label: 'Save an album cover file',
    help: 'Written to the deepest folder whose name uses {AlbumName} or {AlbumId}, else next to the tracks. Default: on.',
  },
  {
    key: 'download.save_artist_cover',
    section: 'artwork',
    type: 'switch',
    label: 'Save an artist picture file',
    help: 'Written to the deepest folder whose name uses an artist variable; skipped when none does. Default: on.',
  },
  { key: 'download.save_playlist_cover', section: 'artwork', type: 'switch', label: 'Save a playlist cover file', help: 'For playlist downloads. Default: on.' },
  // Lyrics
  { key: 'download.embed_lyrics', section: 'lyrics', type: 'switch', label: 'Embed lyrics in each track', help: 'Default: on.' },
  { key: 'download.save_lyrics_file', section: 'lyrics', type: 'switch', label: 'Save a lyrics file next to each track', help: 'Default: off.' },
  {
    key: 'download.lyrics_format',
    section: 'lyrics',
    type: 'select',
    label: 'Lyrics file format',
    help: 'Default: LRC.',
    options: [
      { value: 'lrc', label: 'LRC', hint: 'Apple’s TTML converted to LRC.' },
      { value: 'ttml', label: 'TTML', hint: 'The TTML the wrapper returns, unchanged.' },
    ],
  },
  {
    key: 'download.lyrics_type',
    section: 'lyrics',
    type: 'select',
    label: 'Timing',
    help: 'Default: line by line.',
    options: [
      { value: 'lyrics', label: 'Line by line' },
      { value: 'syllable-lyrics', label: 'Word by word', hint: 'Needs a wrapper that supports word-timed lyrics.' },
    ],
  },
  {
    key: 'download.lyrics_extras',
    section: 'lyrics',
    type: 'set',
    label: 'Extra lines',
    help: 'Localized lines added when Apple has them. Default: none.',
    options: [
      { value: 'translation', label: 'Translation' },
      { value: 'pronunciation', label: 'Pronunciation' },
    ],
  },
  // Catalog
  {
    key: 'catalog.album_track_url_mode',
    section: 'catalog',
    type: 'select',
    label: 'Album links to one song (?i=)',
    help: 'What an album link that points at one track downloads. Default: the song.',
    options: [
      { value: 'song', label: 'The song' },
      { value: 'album', label: 'The whole album' },
    ],
  },
  {
    key: 'catalog.media_user_token',
    section: 'catalog',
    type: 'secret',
    label: 'Media user token',
    help: 'Your Apple Music subscription token (media-user-token). Stations, private playlist covers and library sync need it; a submission may override it. Empty means none. The config API returns it as plain text.',
  },
  {
    key: 'catalog.signed_mode_hls_source',
    section: 'catalog',
    type: 'select',
    label: 'Enhanced HLS source with a signed token',
    help: 'Used only when developer-token signing is set up in the config file; ignored otherwise. Default: wrapper.',
    options: [
      { value: 'wrapper', label: 'Wrapper', hint: 'The authorized device manifest from wrapper-manager.' },
      { value: 'web_token', label: 'Web player token', hint: 'Scrapes a music.apple.com token and reads Enhanced HLS from amp-api.' },
    ],
  },
  {
    key: 'catalog.motion_artwork_enabled',
    section: 'catalog',
    type: 'switch',
    label: 'Look up animated covers',
    help: 'One extra amp-api request per album after a job resolves; it never blocks or fails a download. Off: no request and no motion covers. Default: on.',
  },
  // Library sync
  {
    key: 'library_sync.enabled',
    section: 'library_sync',
    type: 'switch',
    label: 'Watch my library',
    help: 'Polls your library and downloads the album of each newly added song. Needs the media user token (Catalog). Default: off.',
  },
  {
    key: 'library_sync.interval_minutes',
    section: 'library_sync',
    type: 'number',
    label: 'Polling interval',
    help: 'A poll usually makes one Apple request. 1 to 1440. Default: 15.',
    min: 1,
    max: 1440,
    unit: 'min',
  },
  // Logging
  {
    key: 'logging.level',
    section: 'logging',
    type: 'select',
    label: 'Log level',
    help: 'Records below this level are dropped from every output. Default: info.',
    options: [
      { value: 'debug', label: 'Debug' },
      { value: 'info', label: 'Info' },
      { value: 'warn', label: 'Warn' },
      { value: 'error', label: 'Error' },
    ],
  },
  { key: 'logging.access_log', section: 'logging', type: 'switch', label: 'HTTP access log', help: 'One record per completed HTTP request. Default: off.' },
  // Simulate
  {
    key: 'simulate.enabled',
    section: 'simulate',
    type: 'switch',
    label: 'Simulate mode',
    help: 'Jobs skip the transfer, decryption and file writes; catalog data and codec choice stay real, and no wrapper is needed. Default: off.',
  },
  { key: 'simulate.min_speed_kbps', section: 'simulate', type: 'number', label: 'Slowest simulated speed', help: 'At least 1. Default: 512.', min: 1, unit: 'KB/s' },
  { key: 'simulate.max_speed_kbps', section: 'simulate', type: 'number', label: 'Fastest simulated speed', help: 'At least the slowest speed. Default: 4096.', min: 1, unit: 'KB/s' },
];

/**
 * RuntimeConfig keys the page shows no field for, each with the reason.
 * Empty: every runtime key is editable here.
 */
export const NOT_EDITED = {};

export const FIELD_KEYS = FIELDS.map((f) => f.key);
export const BY_KEY = new Map(FIELDS.map((f) => [f.key, f]));

export const fieldByKey = (key) => BY_KEY.get(key);
export const sectionFields = (id) => FIELDS.filter((f) => f.section === id);
