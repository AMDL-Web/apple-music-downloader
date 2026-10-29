// Settings > Account: the session service (wrapper-manager) status, the
// logged-in accounts with Log out, and the login form with its two-step code
// step. While the service cannot be reached, one line with Refresh replaces
// all of it. `children` (the media user token field) follows as the last block.

import { html, useState } from '../ui/preact.js';
import { getWrapperStatus, loginWrapper, submitWrapperTwoStep, logoutWrapper, settle } from '../api.js';
import { wrapperErrorText, wrapperUnreachable, unreachableLine } from '../lib/settings-account.js';
import { wrapperView } from '../lib/status.js';
import { useApp, Badge, Button, Icon, KV, Notice, SkeletonRows } from '../ui/components.js';
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
  const status = usePoll(() => settle(getWrapperStatus()), 30000, [app.epoch]);
  const res = status.data;
  // Login needs the session service; say why it cannot be used instead of failing late.
  const down = res && res.error ? wrapperUnreachable(res.error) : null;
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
  if (!res) state = html`<${SkeletonRows} n=${3} kind="line" />`;
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
  const accounts = res && res.data && Array.isArray(res.data.accounts) ? res.data.accounts : [];

  return html`<div class="account">
    <section class="account-block" aria-labelledby="account-status-title">
      <div class="account-block-head">
        <h3 class="section-title" id="account-status-title">Session service</h3>
        <${Button} variant="ghost" size="sm" icon="rotate" onClick=${status.reload}>Refresh</${Button}>
      </div>
      ${state}
    </section>
    ${res && res.data
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
            : html`<p class="muted small">No account is logged in. Log in below so downloads can be decrypted.</p>`}
        </section>`
      : null}
    <${LoginForm} onDone=${status.reload} again=${accounts.length > 0} />
    ${logout
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

function LogoutDialog({ account, onClose, onDone }) {
  const app = useApp();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const confirm = async () => {
    setBusy(true);
    setError(null);
    try {
      await logoutWrapper(account);
      app.toast({ message: `Logged out ${account}`, role: 'success' });
      onDone();
    } catch (e) {
      setError(e);
      setBusy(false);
    }
  };
  return html`<${ConfirmDialog}
    title="Log out of this account?"
    confirmLabel=${error ? 'Try again' : 'Log out'}
    busyLabel="Logging out…"
    danger
    busy=${busy}
    onConfirm=${confirm}
    onCancel=${onClose}
  >
    <p>The session service ends the login of <span class="mono">${account}</span>. Downloads from its storefronts stop until an account is logged in again.</p>
    <${WrapperError} error=${error} step="logout" />
  </${ConfirmDialog}>`;
}

/**
 * The login form: Apple ID and password, then the two-step code when the
 * wrapper answers 202 needs_2fa. The password goes to the session service
 * and is kept only in this form's state; it is cleared on success.
 */
function LoginForm({ onDone, again }) {
  const app = useApp();
  const [step, setStep] = useState('credentials');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [loginId, setLoginId] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [shown, setShown] = useState(false);

  const reset = () => {
    setShown(false);
    setStep('credentials');
    setPassword('');
    setCode('');
    setLoginId('');
    setError(null);
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
    setError(null);
    try {
      const res = await loginWrapper({ username: username.trim(), password });
      if (res && res.status === 'needs_2fa' && res.login_id) {
        setLoginId(res.login_id);
        setStep('code');
      } else succeeded();
    } catch (err) {
      if (err.status === 401) setPassword('');
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const submitCode = async (e) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await submitWrapperTwoStep(loginId, code.trim());
      succeeded();
    } catch (err) {
      if (err.status === 404) {
        reset();
        setError(err);
      } else {
        setCode('');
        setError(err);
      }
    } finally {
      setBusy(false);
    }
  };

  if (step === 'code') {
    return html`<form class="account-block login-form" data-step="code" aria-labelledby="login-title" onSubmit=${submitCode}>
      <h3 class="section-title" id="login-title">Two-step verification</h3>
      <p class="small">Apple sent a code to the trusted devices of <span class="mono">${username.trim()}</span>. Enter it to finish logging in.</p>
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
      <${WrapperError} error=${error} step="code" />
      <div class="form-actions">
        <${Button} type="submit" variant="primary" aria-disabled=${busy ? 'true' : undefined}>${busy ? 'Verifying…' : 'Verify'}</${Button}>
        <${Button} variant="ghost" onClick=${reset} disabled=${busy}>Start over</${Button}>
      </div>
    </form>`;
  }

  return html`<form class="account-block login-form" data-step="credentials" aria-labelledby="login-title" onSubmit=${submitCredentials}>
    <h3 class="section-title" id="login-title">${again ? 'Log in another account' : 'Log in'}</h3>
    <p class="muted small">The password goes to the session service, which keeps the login; this service does not store it. Logging in can take up to two minutes.</p>
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
          icon=${shown ? 'eye-off' : 'eye'}
          className="field-secret-toggle"
          aria-label=${shown ? 'Hide the password' : 'Show the password'}
          aria-pressed=${String(shown)}
          aria-controls="login-pass"
          onClick=${() => setShown((v) => !v)}
        />
      </span>
    </div>
    <${WrapperError} error=${error} step="login" />
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
