// Whether a download has a media-user-token to use. The wrapper holds every
// account's token, so one exists when the wrapper status
// (GET /wrapper/status) says an account is signed in. Pure.

/**
 * `wrapper` is the status store's wrapper entry ({data, error} or
 * undefined). `known` is false until the wrapper has answered or failed;
 * then `present` says whether an account is signed in, and `key` changes
 * whenever that does ('' while unknown).
 */
export function tokenState(wrapper) {
  const data = wrapper && wrapper.data;
  if (!wrapper || (data === undefined && !wrapper.error)) return { known: false, present: false, key: '' };
  const present = Boolean(data && data.logged_in === true);
  return { known: true, present, key: present ? 'signed-in' : 'signed-out' };
}
