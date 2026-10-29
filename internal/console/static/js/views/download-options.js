// New download: the Options card. Collapsed it shows one line naming what
// differs from the server's download defaults; open it edits the quality and
// whether existing files are overwritten, for this download only. Everything
// else is a Settings value. The page owns the values and turns them into
// overrides (lib/overrides.js); this card only renders and edits.

import { html, useRef, useLayoutEffect } from '../ui/preact.js';
import { CODECS, SAMPLE_RATES, BIT_DEPTHS, choices, moveItem, rateLabel, describeOverrides } from '../lib/overrides.js';
import { settingsHash } from '../lib/route.js';
import { Card, Button, ErrorNote, Notice, SkeletonRows } from '../ui/components.js';

const sameList = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);
const changed = (key, values, defaults) => {
  const a = values[key];
  const b = defaults[key];
  if (Array.isArray(a)) return !sameList(a, b);
  return a !== b;
};

/**
 * Props: cfg ({loading, error, reload}), defaults and values (null until the
 * config loads), setValue(key, value), overrides (what a submit would send),
 * open, setOpen, onReset, refusal (the server's invalid_overrides message).
 */
export function OptionsCard(props) {
  const { cfg, values, overrides, open, setOpen, onReset, refusal } = props;
  const changes = describeOverrides(overrides);
  const summary = changes.length ? changes.join(' · ') : 'Server defaults';
  const actions = [
    changes.length && values
      ? html`<${Button} key="reset" variant="ghost" size="sm" icon="rotate" className="new-options-reset" onClick=${onReset}>Reset</${Button}>`
      : null,
    html`<${Button}
      key="toggle"
      variant="secondary"
      size="sm"
      className="new-options-toggle"
      aria-expanded=${String(open)}
      aria-controls="new-options-body"
      onClick=${() => setOpen(!open)}
    >${open ? 'Done' : 'Edit'}</${Button}>`,
  ];
  let body;
  if (cfg.loading && !values) body = html`<${SkeletonRows} n=${3} kind="line" />`;
  else if (!values)
    body = html`<${ErrorNote} title="Couldn't load the server defaults" error=${cfg.error} onRetry=${cfg.reload} />
      <p class="new-hint">Downloads use the server's settings until the defaults load.</p>`;
  else if (!open)
    body = html`<p class="new-options-summary" data-changed=${changes.length ? '' : undefined} title=${summary}>
      ${changes.length ? html`<span class="new-options-count">${changes.length} ${changes.length === 1 ? 'change' : 'changes'}</span>` : null}
      <span class="new-options-text">${summary}</span>
    </p>`;
  else body = html`<${OptionsForm} ...${props} />`;
  return html`<${Card} section="options" className="new-options" title="Options" labelId="new-options-title" actions=${actions}>
    ${refusal ? html`<${Notice} role="danger" icon="circle-x"><strong>The server refused these options.</strong> ${refusal}</${Notice}>` : null}
    <div id="new-options-body" class="new-options-body" data-open=${open ? '' : undefined}>${body}</div>
  </${Card}>`;
}

function Check({ name, checked, onChange, children, hint, disabled, changedMark }) {
  return html`<label class="new-check" data-changed=${changedMark ? '' : undefined} data-key=${name}>
    <input type="checkbox" checked=${checked} disabled=${disabled} onChange=${(e) => onChange(e.currentTarget.checked)} />
    <span class="new-check-text">
      <span class="new-check-label">${children}</span>
      ${hint ? html`<span class="new-check-hint">${hint}</span>` : null}
    </span>
  </label>`;
}

function Select({ name, labelText, value, options, format, onChange, changedMark }) {
  const id = `new-opt-${name}`;
  return html`<div class="new-field" data-changed=${changedMark ? '' : undefined} data-key=${name}>
    <label class="new-field-label" for=${id}>${labelText}</label>
    <select id=${id} class="select" value=${String(value)} onChange=${(e) => onChange(e.currentTarget.value)}>
      ${options.map((o) => html`<option key=${String(o)} value=${String(o)}>${format(o)}</option>`)}
    </select>
  </div>`;
}

function OptionsForm({ defaults, values, setValue, onReset }) {
  const ch = (key) => changed(key, values, defaults);
  const bool = (key, text, hint) => html`<${Check} key=${key} name=${key} checked=${values[key]} changedMark=${ch(key)} hint=${hint} onChange=${(v) => setValue(key, v)}>${text}</${Check}>`;
  const num = (v) => Number.parseInt(v, 10);
  return html`<div class="new-options-form">
    <fieldset class="new-fieldset" data-group="quality">
      <legend class="section-title">Quality</legend>
      <${CodecPriority} list=${values.quality_priority} changedMark=${ch('quality_priority')} onChange=${(v) => setValue('quality_priority', v)} />
      ${bool('codec_alternative', 'Fall back to the next codec', 'Off: only the first codec is tried, then AAC-LC.')}
      ${values.quality_priority.includes('alac')
        ? html`<div class="new-field-row">
        <${Select}
          name="alac_max_sample_rate"
          labelText="Highest ALAC sample rate"
          value=${values.alac_max_sample_rate}
          options=${choices(SAMPLE_RATES, defaults.alac_max_sample_rate)}
          format=${rateLabel}
          changedMark=${ch('alac_max_sample_rate')}
          onChange=${(v) => setValue('alac_max_sample_rate', num(v))}
        />
        <${Select}
          name="alac_max_bit_depth"
          labelText="Highest ALAC bit depth"
          value=${values.alac_max_bit_depth}
          options=${choices(BIT_DEPTHS, defaults.alac_max_bit_depth)}
          format=${(v) => `${v}-bit`}
          changedMark=${ch('alac_max_bit_depth')}
          onChange=${(v) => setValue('alac_max_bit_depth', num(v))}
        />
      </div>`
        : null}
    </fieldset>
    <fieldset class="new-fieldset" data-group="behavior">
      <legend class="section-title">Files</legend>
      ${bool('force_overwrite', 'Overwrite files that already exist', 'Off: tracks already on disk are skipped.')}
    </fieldset>
    <div class="new-options-foot">
      <p class="new-hint">Lyrics, covers, folders and retries come from <a href=${settingsHash()}>Settings</a>.</p>
      <${Button} variant="ghost" size="sm" icon="rotate" className="new-options-reset" onClick=${onReset}>Reset to defaults</${Button}>
    </div>
  </div>`;
}

/**
 * The ordered codec list: move up and down, remove (never the last), add an
 * excluded codec. Focus stays in the list: an added codec's row takes it,
 * and a removed row hands it to its neighbour.
 */
function CodecPriority({ list, onChange, changedMark }) {
  const box = useRef(null);
  const pending = useRef([]);
  const excluded = CODECS.filter((c) => !list.includes(c.value));
  const info = (v) => CODECS.find((c) => c.value === v) || { value: v, label: v, hint: '' };
  // Moves focus right after the list re-renders, before the browser can
  // drop it on the body with the removed button.
  const focusLater = (...selectors) => {
    pending.current = selectors;
  };
  useLayoutEffect(() => {
    const selectors = pending.current;
    pending.current = [];
    if (!box.current) return;
    for (const selector of selectors) {
      const el = box.current.querySelector(selector);
      if (el && !el.disabled) {
        el.focus();
        return;
      }
    }
  }, [list.join(',')]);
  const add = (v) => {
    onChange([...list, v]);
    focusLater(`.new-codec[data-codec="${v}"] .new-codec-up`);
  };
  // Focus stays on the moved row's same arrow, or the other one at an end,
  // so the next press moves it again.
  const move = (i, d) => {
    const v = list[i];
    onChange(moveItem(list, i, d));
    const row = `.new-codec[data-codec="${v}"]`;
    focusLater(`${row} .${d < 0 ? 'new-codec-up' : 'new-codec-down'}`, `${row} .${d < 0 ? 'new-codec-down' : 'new-codec-up'}`);
  };
  const remove = (i) => {
    const next = list.filter((_, j) => j !== i);
    onChange(next);
    const neighbour = next[Math.min(i, next.length - 1)];
    focusLater(next.length > 1 ? `.new-codec[data-codec="${neighbour}"] .new-codec-remove` : '.new-codec-add-btn');
  };
  const only = list.length === 1 ? info(list[0]).label : '';
  return html`<div class="new-codecs" ref=${box} data-changed=${changedMark ? '' : undefined} data-key="quality_priority">
    <span class="new-field-label" id="new-codecs-label">Codec priority</span>
    <ol class="new-codec-list" aria-labelledby="new-codecs-label" aria-describedby="new-codecs-hint">
      ${list.map((v, i) => {
        const c = info(v);
        return html`<li key=${v} class="new-codec" data-codec=${v}>
          <span class="new-codec-rank" aria-hidden="true">${i + 1}</span>
          <span class="new-codec-name">${c.label}<span class="new-codec-hint">${c.hint}</span></span>
          <span class="new-codec-actions">
            <${Button} variant="ghost" size="sm" icon="arrow-down" className="new-codec-up" aria-label=${`Move ${c.label} up`} title="Move up" disabled=${i === 0} onClick=${() => move(i, -1)} />
            <${Button} variant="ghost" size="sm" icon="arrow-down" className="new-codec-down" aria-label=${`Move ${c.label} down`} title="Move down" disabled=${i === list.length - 1} onClick=${() => move(i, 1)} />
            <${Button} variant="ghost" size="sm" icon="x" className="new-codec-remove" aria-label=${`Remove ${c.label}`} title=${only ? 'At least one codec is needed' : 'Remove'} disabled=${Boolean(only)} onClick=${() => remove(i)} />
          </span>
        </li>`;
      })}
    </ol>
    ${excluded.length
      ? html`<div class="new-codec-add" role="group" aria-label="Add a codec">
          <span class="new-codec-add-label">Add</span>
          ${excluded.map(
            (c) => html`<${Button} key=${c.value} variant="secondary" size="sm" icon="plus" className="new-codec-add-btn" data-codec=${c.value} title=${c.hint} onClick=${() => add(c.value)}>${c.label}</${Button}>`,
          )}
        </div>`
      : null}
    <p class="new-hint" id="new-codecs-hint">
      ${only ? `At least one codec is needed: add another before removing ${only}. ` : ''}Tried top to bottom. AAC-LC, the plain AAC every track has, is always the last fallback and needs no entry.
    </p>
  </div>`;
}
