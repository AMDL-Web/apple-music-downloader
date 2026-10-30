// Settings > Account, below its status head: the logged-in accounts with
// Log out, and the login form with its two-step code step. A session service
// without a login API (a bare wrapper-lite) gets the command-line steps
// instead of the form. While the service cannot be reached, the server's
// own text replaces all of it; a wrapper amdl runs that failed adds its
// error, one that is stopped keeps its form disabled (a login needs it
// running), and one whose guest disk may hold the last password says so.
// `children` (the media user token field) follows as the last block.

import { html, useState } from '../ui/preact.js';
import { loginWrapper, submitWrapperTwoStep, logoutWrapper } from '../api.js';
import { wrapperErrorText, wrapperUnreachable, managedMode, WRAPPER_LOGS } from '../lib/settings-account.js';
import { useApp, Button, Notice, Icon } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { useStatus, refreshStatus } from '../ui/system-status.js';

/** A failed wrapper call as a notice: the message, then the server's text in mono. */
function WrapperError({ error, step, role = 'danger' }) {
  if (!error) return null;
  const t = wrapperErrorText(error, step);
  return html`<${Notice} role=${role} icon=${role === 'danger' ? 'circle-x' : 'triangle-alert'}>
    <span class="account-error-msg">${t.message}</span>
    ${t.detail ? html`<details class="account-error-details"><summary>Details from the server</summary><span class="mono small wrap-any account-error-detail">${t.detail}</span></details>` : null}
  </${Notice}>`;
}

export function AccountPanel({ children }) {
  const res = useStatus(['wrapper']).wrapper;
  const reload = () => refreshStatus('wrapper');
  // Login needs the session service; say why it cannot be used instead of failing late.
  const down = res && res.error ? wrapperUnreachable(res.error) : null;
  // null: no dialog; '': ask for the Apple ID; else the account to log out.
  const [logout, setLogout] = useState(null);

  // The section head says the service is unreachable, its note the error in
  // a line with the server's own words as its tooltip; the body has nothing
  // more to add.
  if (down) {
    return html`<div class="account">${children}</div>`;
  }

  const w = res && !res.error ? res.data : null;
  const accounts = w && Array.isArray(w.accounts) ? w.accounts : [];
  const canLogin = !w || w.login_supported !== false;
  const managed = w && w.managed ? w.managed : null;
  const mode = managedMode(w);
  const failure = mode === 'failed' ? managed.last_error || '' : null;

  return html`<div class="account">
    ${managed && managed.login_residue
      ? html`<p class="small account-residue"><${Icon} name="lock" size="sm" /><span>The guest's disk may still hold the last login's password until amdl clears it before the next start.</span></p>`
      : null}
    ${res && res.error
      ? html`<section class="account-block" aria-label="Session service"><${WrapperError} error=${res.error} step="status" role="warning" /></section>`
      : null}
    ${failure !== null
      ? html`<section class="account-block" aria-label="Session service">
          ${failure ? html`<details class="account-error-details"><summary>Details from the server</summary><span class="mono small wrap-any account-error-detail">${failure}</span></details>` : null}
          <p class="small">Its log: <code>${WRAPPER_LOGS}</code></p>
        </section>`
      : null}
    ${w && (accounts.length || w.logged_in)
      ? html`<section class="account-block" aria-labelledby="account-list-title">
          <div class="account-block-head">
            <h3 class="section-title" id="account-list-title">Logged-in accounts</h3>
          </div>
          ${accounts.length
            ? html`<ul class="account-rows">
                ${accounts.map(
                  (a) => html`<li key=${a} class="account-row">
                    <span class="account-name mono">${a}</span>
                    ${canLogin ? html`<${Button} variant="secondary" size="sm" className="account-logout" onClick=${() => setLogout(a)}>Log out</${Button}>` : null}
                  </li>`,
                )}
              </ul>`
            : w.logged_in
              ? html`<p class="small">An account is logged in. The session service does not say which one.</p>
                  ${canLogin
                    ? html`<div class="form-actions">
                        <${Button} variant="secondary" size="sm" className="account-logout" onClick=${() => setLogout('')}>Log out…</${Button}>
                      </div>`
                    : null}`
              : null}
        </section>`
      : null}
    ${!res
      ? html`<${CommandLineLogin} pending />`
      : canLogin
        ? html`<${LoginForm} onDone=${reload} again=${accounts.length > 0 || Boolean(w && w.logged_in)} managed=${Boolean(managed)} off=${mode === 'stopped'} />`
        : w && w.logged_in
          ? null
          : html`<${CommandLineLogin} />`}
    ${logout !== null
      ? html`<${LogoutDialog}
          account=${logout}
          onClose=${() => setLogout(null)}
          onDone=${() => {
            setLogout(null);
            reload();
          }}
        />`
      : null}
    ${children}
  </div>`;
}

/** Log out `account`, or the Apple ID typed in when `account` is '' (no wrapper lists its accounts). */
function LogoutDialog({ account, onClose, onDone }) {
  const app = useApp();
  const [name, setName] = useState(account);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [missing, setMissing] = useState(false);
  const asked = account === '';
  const target = name.trim();
  const confirm = async () => {
    if (!target) {
      setMissing(true);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await logoutWrapper(target);
      app.toast({ message: `Logged out ${target}`, role: 'success' });
      onDone();
    } catch (e) {
      setError(e);
      setBusy(false);
    }
  };
  return html`<${ConfirmDialog}
    title=${asked ? 'Log out of an account?' : 'Log out of this account?'}
    confirmLabel=${error ? 'Try again' : 'Log out'}
    busyLabel="Logging out…"
    danger
    busy=${busy}
    onConfirm=${confirm}
    onCancel=${onClose}
  >
    ${asked
      ? html`<p>The session service does not list its accounts. Enter the Apple ID to log out; its login ends and downloads from its storefronts stop until an account is logged in again.</p>
          <div class="form-row">
            <label class="field-label" for="logout-user">Apple ID</label>
            <input
              id="logout-user"
              class="field-input"
              type="text"
              name="username"
              inputmode="email"
              autocomplete="username"
              autocapitalize="off"
              spellcheck="false"
              aria-invalid=${missing && !target ? 'true' : undefined}
              data-autofocus
              value=${name}
              onInput=${(e) => {
                setName(e.currentTarget.value);
                setMissing(false);
              }}
              onKeyDown=${(e) => {
                if (e.key === 'Enter' && !busy) confirm();
              }}
            />
            ${missing && !target ? html`<p class="field-help" role="alert">Enter the Apple ID to log out.</p>` : null}
          </div>`
      : html`<p>The session service ends the login of <span class="mono">${account}</span>. Downloads from its storefronts stop until an account is logged in again.</p>`}
    <${WrapperError} error=${error} step="logout" />
  </${ConfirmDialog}>`;
}

/**
 * What replaces the login form when the session service has no login API
 * and no account is logged in. `pending` holds, unseen, the room of the
 * usual answer while the status loads (a bare wrapper-lite with its account
 * logged in: one line), so that answer moves nothing.
 */
function CommandLineLogin({ pending }) {
  if (pending) {
    return html`<div class="account-block holds-room" aria-hidden="true">
      <h3 class="section-title">Logged-in accounts</h3>
      <p class="small">An account is logged in. The session service does not say which one.</p>
    </div>`;
  }
  return html`<section class="account-block" aria-labelledby="login-title">
    ${CommandLineSteps('login-title')}
  </section>`;
}

/** The command-line login steps; `id` names the heading when it labels the section. */
function CommandLineSteps(id) {
  return html`<h3 class="section-title" id=${id}>Log in from the command line</h3>
    <p class="small">This session service is a bare wrapper-lite, which has no login of its own. On the machine that runs it, log in once with <code>--login</code>; it saves the login and exits:</p>
    <code class="account-cmd mono">wrapper-lite-rootless --login APPLE_ID:PASSWORD</code>
    <p class="muted small">It asks for the two-step code in the terminal; without one, add <code>--code-from-file</code> and write the code to <code>2fa.txt</code> in its data folder. Then start it again without <code>--login</code> and select Refresh above.</p>`;
}

/**
 * The login form: Apple ID and password, then the two-step code when the
 * wrapper answers 202 needs_2fa. The password goes to the session service;
 * this page keeps it only in this form's state, and clears it on success and
 * when a code fails (the backend drops its copy then too, so the form starts
 * over from the password).
 */
function LoginForm({ onDone, again, managed, off }) {
  const app = useApp();
  const [step, setStep] = useState('credentials');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [loginId, setLoginId] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState(null);
  const [shown, setShown] = useState(false);
  const [shortCode, setShortCode] = useState(false);

  const reset = () => {
    setShown(false);
    setStep('credentials');
    setPassword('');
    setCode('');
    setShortCode(false);
    setLoginId('');
    setFailure(null);
  };
  const succeeded = () => {
    app.toast({ message: `Logged in ${username.trim()}`, role: 'success' });
    reset();
    setUsername('');
    onDone();
  };

  const submitCredentials = async (e) => {
    e.preventDefault();
    if (busy || off) return;
    setBusy(true);
    setFailure(null);
    try {
      const res = await loginWrapper({ username: username.trim(), password });
      if (res && res.status === 'needs_2fa' && res.login_id) {
        setLoginId(res.login_id);
        setStep('code');
      } else succeeded();
    } catch (err) {
      if (err.status === 401) setPassword('');
      setFailure({ error: err, step: 'login' });
    } finally {
      setBusy(false);
    }
  };

  const submitCode = async (e) => {
    e.preventDefault();
    if (busy) return;
    // A code that is not six digits never reaches Apple, and the sign-in waits on.
    if (!/^[0-9]{6}$/.test(code)) {
      setShortCode(true);
      return;
    }
    setBusy(true);
    setFailure(null);
    try {
      await submitWrapperTwoStep(loginId, code);
      succeeded();
    } catch (err) {
      // The backend ends the sign-in on any failed attempt except a code it
      // refuses to pass on (400) or a second submit while one is being
      // verified (409), so otherwise start over from the password.
      if (err.status === 400 || err.status === 409) setFailure({ error: err, step: 'code' });
      else {
        reset();
        setFailure({ error: err, step: 'code' });
      }
    } finally {
      setBusy(false);
    }
  };

  if (step === 'code') {
    return html`<form class="account-block login-form" data-step="code" aria-labelledby="login-title" onSubmit=${submitCode}>
      <h3 class="section-title" id="login-title">Two-step verification</h3>
      <p class="small">Apple sent a code to the trusted devices of <span class="mono">${username.trim()}</span>. Enter it within ${managed ? 'a few minutes' : 'a minute'} to finish logging in.</p>
      <div class="form-row">
        <label class="field-label" for="login-code">Verification code</label>
        <input
          id="login-code"
          class="field-input mono login-code"
          type="text"
          inputmode="numeric"
          autocomplete="one-time-code"
          spellcheck="false"
          required
          aria-invalid=${shortCode ? 'true' : undefined}
          aria-describedby=${shortCode ? 'login-code-error' : undefined}
          data-autofocus
          value=${code}
          onInput=${(e) => {
            setCode(e.currentTarget.value.replace(/\D/g, '').slice(0, 6));
            setShortCode(false);
          }}
        />
        ${shortCode ? html`<p class="field-error" id="login-code-error" role="alert">Enter the six-digit code.</p>` : null}
      </div>
      <${WrapperError} error=${failure && failure.error} step="code" />
      <div class="form-actions">
        <${Button} type="submit" variant="primary" aria-disabled=${busy ? 'true' : undefined}>${busy ? 'Verifying…' : 'Verify'}</${Button}>
        <${Button} variant="ghost" onClick=${reset} disabled=${busy}>Start over</${Button}>
      </div>
    </form>`;
  }

  return html`<form class="account-block login-form" data-step="credentials" aria-labelledby="login-title" onSubmit=${submitCredentials}>
    <h3 class="section-title" id="login-title">${!again ? 'Log in' : managed ? 'Replace the account' : 'Log in another account'}</h3>
    <p class="muted small">${managed ? 'A login replaces the current account and takes several minutes.' : 'Logging in can take up to two minutes.'}</p>
    <div class="form-row">
      <label class="field-label" for="login-user">Apple ID</label>
      <p class="field-help" id="login-user-help">The email address or phone number you sign in to Apple Music with.</p>
      <input
        id="login-user"
        class="field-input"
        type="text"
        name="username"
        inputmode="email"
        autocomplete="username"
        autocapitalize="off"
        aria-describedby="login-user-help"
        spellcheck="false"
        required
        disabled=${off}
        value=${username}
        onInput=${(e) => setUsername(e.currentTarget.value)}
      />
    </div>
    <div class="form-row">
      <label class="field-label" for="login-pass">Password</label>
      <span class="field-secret">
        <input
          id="login-pass"
          class="field-input"
          type=${shown ? 'text' : 'password'}
          name="password"
          autocomplete="current-password"
          autocapitalize="off"
          spellcheck="false"
          required
          disabled=${off}
          value=${password}
          onInput=${(e) => setPassword(e.currentTarget.value)}
        />
        <${Button}
          variant="ghost"
          size="sm"
          icon=${shown ? 'eye-off' : 'eye'}
          className="field-secret-toggle"
          aria-label=${shown ? 'Hide the password' : 'Show the password'}
          aria-pressed=${String(shown)}
          aria-controls="login-pass"
          disabled=${off}
          onClick=${() => setShown((v) => !v)}
        />
      </span>
    </div>
    <${WrapperError} error=${failure && failure.error} step=${failure ? failure.step : 'login'} />
    <div class="form-actions">
      <${Button}
        type="submit"
        variant="primary"
        icon="key"
        disabled=${off}
        aria-disabled=${busy ? 'true' : undefined}
      >${busy ? 'Logging in…' : 'Log in'}</${Button}>
    </div>
  </form>`;
}
