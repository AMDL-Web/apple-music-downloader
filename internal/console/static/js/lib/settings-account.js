// Messages for the wrapper account calls the Settings page makes (status,
// login, two-step code, logout), keyed by the API's error codes. Pure.

/**
 * Whether a failed wrapper call means the wrapper-manager service itself
 * cannot be reached (as opposed to Apple refusing a login): the API's
 * wrapper_unavailable code or a raw gRPC "Unavailable" text.
 * Returns null otherwise, else {address, detail}: the address read from the
 * dial error when there is one ('' when not), and the server's own text.
 */
export function wrapperUnreachable(err) {
  if (!err || !err.status) return null;
  const raw = err.message || '';
  const grpc = /code = Unavailable/.test(raw);
  if (err.code !== 'wrapper_unavailable' && !grpc) return null;
  const m = /dial tcp (\S+?):\s/.exec(raw);
  return { address: m ? m[1] : '', detail: raw };
}

/** The Account section's one line while the session service cannot be reached. */
export function unreachableLine(u) {
  return `Can't reach the session service${u.address ? ` at ${u.address}` : ''}.`;
}

/**
 * The plain-words sentence for an unreachable session service. The page says
 * "session service"; wrapper-manager, the program behind it, is named only
 * in the server's own text under "Details from the server".
 */
export function unreachableText(u) {
  return `The Apple Music session service ${u.address ? `at ${u.address} ` : ''}is not reachable.`;
}

/**
 * Text for a failed wrapper call (an ApiError). `step` is login, code (the
 * two-step code), logout or status. {message, detail}: detail is the
 * server's own text when it adds something.
 */
export function wrapperErrorText(err, step = 'login') {
  const raw = (err && err.message) || '';
  const status = err ? err.status : 0;
  const code = (err && err.code) || '';
  const down = wrapperUnreachable(err);
  if (down) return { message: unreachableText(down), detail: raw };
  if (status === 0) {
    return { message: code === 'timeout' ? 'The session service did not answer in time. Try again.' : 'Cannot reach the server.', detail: '' };
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
      return { message: 'This account is not logged in to the session service.', detail: '' };
    case 'upstream_timeout':
      return { message: 'The session service did not answer in time. Try again.', detail: raw };
    case 'upstream_error':
      return { message: 'The session service could not complete the request.', detail: raw };
    case 'wrapper_unavailable':
      return { message: 'The session service is not reachable.', detail: raw };
    default:
      break;
  }
  if (status === 400) return { message: raw || 'The request was not valid.', detail: '' };
  if (status === 401) return wrapperErrorText({ status, code: 'authentication_failed' }, step);
  if (status === 404) return { message: step === 'code' ? 'This sign-in expired or already finished. Start again.' : 'Not found.', detail: raw };
  if (status === 409) return { message: 'The session service is busy with this account.', detail: raw };
  if (status === 502) return { message: 'The session service could not complete the request.', detail: raw };
  if (status === 503) return { message: 'The session service is not reachable.', detail: raw };
  if (status === 504) return { message: 'The session service did not answer in time. Try again.', detail: raw };
  return { message: raw || `The request failed with HTTP ${status}.`, detail: '' };
}
