// Messages for the wrapper account calls the Settings page makes (status,
// login, two-step code, logout), keyed by the API's error codes. Pure.

/**
 * Text for a failed wrapper call (an ApiError). `step` is login, code (the
 * two-step code), logout or status. {message, detail}: detail is the
 * server's own text when it adds something.
 */
export function wrapperErrorText(err, step = 'login') {
  const raw = (err && err.message) || '';
  const status = err ? err.status : 0;
  const code = (err && err.code) || '';
  if (status === 0) {
    return { message: code === 'timeout' ? 'The wrapper did not answer in time. Try again.' : 'Cannot reach the server.', detail: '' };
  }
  switch (code) {
    case 'authentication_failed':
      return { message: step === 'code' ? 'Apple did not accept this code. Check it and try again.' : 'Apple did not accept this Apple ID and password.', detail: '' };
    case 'already_logged_in':
      return { message: 'This account is already logged in.', detail: '' };
    case 'login_pending':
      return { message: 'A login for this account is already in progress. Finish it, or wait for it to time out.', detail: '' };
    case 'login_not_found':
      return { message: 'This sign-in expired or already finished. Start again.', detail: '' };
    case 'account_not_found':
      return { message: 'The wrapper has no session for this account.', detail: '' };
    case 'upstream_timeout':
      return { message: 'The wrapper did not answer in time. Try again.', detail: raw };
    case 'upstream_error':
      return { message: 'The wrapper could not complete the request.', detail: raw };
    case 'wrapper_unavailable':
      return { message: 'wrapper-manager is not reachable.', detail: raw };
    case 'feature_unavailable':
      return { message: 'This backend has no wrapper connection.', detail: raw };
    default:
      break;
  }
  if (status === 400) return { message: raw || 'The request was not valid.', detail: '' };
  if (status === 401) return wrapperErrorText({ status, code: 'authentication_failed' }, step);
  if (status === 404) return { message: step === 'code' ? 'This sign-in expired or already finished. Start again.' : 'Not found.', detail: raw };
  if (status === 409) return { message: 'The wrapper is busy with this account.', detail: raw };
  if (status === 502) return { message: 'The wrapper could not complete the request.', detail: raw };
  if (status === 503) return { message: 'wrapper-manager is not reachable.', detail: raw };
  if (status === 504) return { message: 'The wrapper did not answer in time. Try again.', detail: raw };
  return { message: raw || `The request failed with HTTP ${status}.`, detail: '' };
}
