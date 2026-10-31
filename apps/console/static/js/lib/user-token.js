// The media-user-token a download uses: the wrapper's when the backend reads
// one (`media_user_token` of GET /wrapper/status), else the one saved in
// Settings (`catalog.media_user_token` of GET /config). Both arrive masked
// ("..." and the last four characters). Pure.

const masked = (v) => (typeof v === 'string' ? v : '');

/**
 * `saved` is the saved token as GET /config has it, undefined while the
 * config is not loaded; `wrapper` the status store's wrapper entry ({data,
 * error} or undefined). `known` is false until the answer is settled: the
 * config is loaded, and the wrapper has answered or failed unless it already
 * has a token. Then `present` says whether there is a token, `source` is
 * 'wrapper', 'config' or null, and `key` names the token in use, so it
 * changes whenever that token does. `wrapperToken` is the wrapper's, '' when
 * it has none.
 */
export function tokenState(saved, wrapper) {
  const data = wrapper && wrapper.data;
  const wrapperToken = masked(data && data.media_user_token);
  if (wrapperToken) return { known: true, present: true, source: 'wrapper', key: `wrapper${wrapperToken}`, wrapperToken };
  const none = { known: false, present: false, source: null, key: '', wrapperToken: '' };
  if (saved === undefined || saved === null) return none;
  const savedToken = masked(saved);
  if (savedToken) return { known: true, present: true, source: 'config', key: `config${savedToken}`, wrapperToken: '' };
  if (!wrapper || (data === undefined && !wrapper.error)) return none;
  return { ...none, known: true };
}
