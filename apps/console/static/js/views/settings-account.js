// Settings > Account: the session service status, the logged-in accounts with
// Log out, and the login form with its two-step code step. A session service
// without a login API (a bare wrapper-lite) gets the command-line steps
// instead of the form. While the service cannot be reached, one line with
// Refresh replaces all of it. `children` (the media user token field) follows
// as the last block.

import { html, useState } from '../ui/preact.js';
import { getWrapperStatus, loginWrapper, submitWrapperTwoStep, logoutWrapper, settle } from '../api.js';
import { wrapperErrorText, wrapperUnreachable, unreachableLine } from '../lib/settings-account.js';
import { wrapperView } from '../lib/health.js';
import { useApp, Badge, Button, Icon, KV, Notice, SkeletonKV } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { usePoll } from '../ui/hooks.js';

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
  const app = useApp();
  const status = usePoll(() => settle(getWrapperStatus()), 30000, [app.epoch], 'wrapper-status');
  const res = status.data;
  // Login needs the session service; say why it cannot be used instead of failing late.
  const down = res && res.error ? wrapperUnreachable(res.error) : null;
  // null: no dialog; '': ask for the Apple ID; else the account to log out.
  const [logout, setLogout] = useState(null);

  if (down) {
    return html`<div class="account">
      <section class="account-block" aria-label="Session service">
        <p class="account-down" role="status">
          <${Icon} name="triangle-alert" size="sm" />
          <span>${unreachableLine(down)}</span>
          <${Button} variant="ghost" size="sm" icon="rotate" className="account-down-refresh" onClick=${status.reload}>Refresh</${Button}>
        </p>
      </section>
      ${children}
    </div>`;
  }

  let state;
  if (!res) state = html`<${SkeletonKV} labels=${['State', 'Connected', 'Regions']} badges=${['State']} />`;
  else if (res.error) {
    state = html`<${KV} rows=${[['State', html`<${Badge} view=${wrapperView(res)} />`]]} />
      <${WrapperError} error=${res.error} step="status" role="warning" />`;
  } else {
    const w = res.data;
    state = html`<${KV}
      rows=${[
        ['State', html`<${Badge} view=${wrapperView(res)} />`],
        ['Connected', w.status ? 'Yes' : 'No'],
        ['Regions', Array.isArray(w.regions) && w.regions.length ? w.regions.map((r) => r.toUpperCase()).join(', ') : 'None'],
      ]}
    />`;
  }
  const w = res && res.data;
  const accounts = w && Array.isArray(w.accounts) ? w.accounts : [];
  const canLogin = !w || w.login_supported !== false;

  return html`<div class="account">
    <section class="account-block" aria-labelledby="account-status-title">
      <div class="account-block-head">
        <h3 class="section-title" id="account-status-title">Session service</h3>
        <${Button} variant="ghost" size="sm" icon="rotate" onClick=${status.reload}>Refresh</${Button}>
      </div>
      ${state}
    </section>
    ${w && canLogin
      ? html`<section class="account-block" aria-labelledby="account-list-title">
          <div class="account-block-head">
            <h3 class="section-title" id="account-list-title">Logged-in accounts</h3>
          </div>
          ${accounts.length
            ? html`<ul class="account-rows">
                ${accounts.map(
                  (a) => html`<li key=${a} class="account-row">
                    <span class="account-name mono">${a}</span>
                    <${Button} variant="secondary" size="sm" className="account-logout" onClick=${() => setLogout(a)}>Log out</${Button}>
                  </li>`,
                )}
              </ul>`
            : w.status
              ? html`<p class="small">An account is logged in. The session service does not say which one.</p>
                  <div class="form-actions">
                    <${Button} variant="secondary" size="sm" className="account-logout" onClick=${() => setLogout('')}>Log out…</${Button}>
                  </div>`
              : html`<p class="muted small">No account is logged in. Log in below so downloads can be decrypted.</p>`}
        </section>`
      : null}
    ${!res
      ? html`<${CommandLineLogin} pending />`
      : canLogin
        ? html`<${LoginForm} onDone=${status.reload} again=${accounts.length > 0 || Boolean(w && w.status)} />`
        : html`<${CommandLineLogin} />`}
    ${logout !== null
      ? html`<${LogoutDialog}
          account=${logout}
          onClose=${() => setLogout(null)}
          onDone=${() => {
            setLogout(null);
            status.reload();
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
 * What replaces the login form when the session service has no login API.
 * `pending` holds its room, unseen, while the status loads: a bare
 * wrapper-lite is the usual service, so its answer then moves nothing.
 */
function CommandLineLogin({ pending }) {
  if (pending) {
    return html`<div class="account-block holds-room" aria-hidden="true">${CommandLineSteps()}</div>`;
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
    <p class="muted small">It asks for the two-step code in the terminal; without one, add <code>--code-from-file</code> and write the code to <code>2fa.txt</code> in its data folder. Then start it again without <code>--login</code> and select Refresh above. To log in from this page instead, run wrapper-manager v2 in front of it and set <code>wrapper.address</code> to that.</p>`;
}

/**
 * The login form: Apple ID and password, then the two-step code when the
 * wrapper answers 202 needs_2fa. The password goes to the session service;
 * this page keeps it only in this form's state, and clears it on success and
 * when a code fails (the backend drops its copy then too, so the form starts
 * over from the password).
 */
function LoginForm({ onDone, again }) {
  const app = useApp();
  const [step, setStep] = useState('credentials');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [loginId, setLoginId] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState(null);
  const [shown, setShown] = useState(false);

  const reset = () => {
    setShown(false);
    setStep('credentials');
    setPassword('');
    setCode('');
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
    if (busy) return;
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
    setBusy(true);
    setFailure(null);
    try {
      await submitWrapperTwoStep(loginId, code.trim());
      succeeded();
    } catch (err) {
      // The backend ends the sign-in on any failed attempt except a second
      // submit while one is being verified (409), so start over from the password.
      if (err.status === 409) setFailure({ error: err, step: 'code' });
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
      <p class="small">Apple sent a code to the trusted devices of <span class="mono">${username.trim()}</span>. Enter it within a minute to finish logging in.</p>
      <div class="form-row">
        <label class="field-label" for="login-code">Verification code</label>
        <input
          id="login-code"
          class="field-input mono login-code"
          type="text"
          inputmode="numeric"
          autocomplete="one-time-code"
          spellcheck="false"
          maxlength="8"
          required
          data-autofocus
          value=${code}
          onInput=${(e) => setCode(e.currentTarget.value)}
        />
      </div>
      <${WrapperError} error=${failure && failure.error} step="code" />
      <div class="form-actions">
        <${Button} type="submit" variant="primary" aria-disabled=${busy ? 'true' : undefined}>${busy ? 'Verifying…' : 'Verify'}</${Button}>
        <${Button} variant="ghost" onClick=${reset} disabled=${busy}>Start over</${Button}>
      </div>
    </form>`;
  }

  return html`<form class="account-block login-form" data-step="credentials" aria-labelledby="login-title" onSubmit=${submitCredentials}>
    <h3 class="section-title" id="login-title">${again ? 'Log in another account' : 'Log in'}</h3>
    <p class="muted small">The session service keeps the login. This service holds the password in memory only while you enter a two-step code, and never stores it. Logging in can take up to two minutes.</p>
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
        aria-disabled=${busy ? 'true' : undefined}
      >${busy ? 'Logging in…' : 'Log in'}</${Button}>
    </div>
  </form>`;
}
