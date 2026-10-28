// New download: the Options card. Collapsed it shows one line naming what
// differs from the server's download defaults; open it edits quality,
// lyrics, covers, attempts and hooks. The page owns the values and turns
// them into overrides (lib/overrides.js); this card only renders and edits.

import { html } from '../ui/preact.js';
import {
  CODECS,
  SAMPLE_RATES,
  BIT_DEPTHS,
  COVER_SIZES,
  COVER_FORMATS,
  LYRICS_FORMATS,
  LYRICS_TYPES,
  LYRICS_EXTRAS,
  MAX_ATTEMPTS,
  choices,
  moveItem,
  rateLabel,
  describeOverrides,
} from '../lib/overrides.js';
import { Card, Button, ErrorNote, Notice, SkeletonRows, Unavailable } from '../ui/components.js';

const LABELS = {
  lrc: 'LRC',
  ttml: 'TTML',
  lyrics: 'Line by line',
  'syllable-lyrics': 'Word by word',
  translation: 'Translation',
  pronunciation: 'Pronunciation',
  jpg: 'JPEG',
  jpeg: 'JPEG (jpeg)',
  png: 'PNG',
};
const label = (v) => LABELS[v] || String(v);
const sizeLabel = (v) => String(v).replace('x', ' × ');
const ATTEMPTS = Array.from({ length: MAX_ATTEMPTS }, (_, i) => i + 1);

const sameList = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);
const changed = (key, values, defaults) => {
  const a = values[key];
  const b = defaults[key];
  if (Array.isArray(a)) return !sameList(key === 'lyrics_extras' ? [...a].sort() : a, key === 'lyrics_extras' ? [...b].sort() : b);
  return a !== b;
};

/**
 * Props: cfg ({loading, error, reload}), defaults and values (null until the
 * config loads), setValue(key, value), hooks ({loading, data, error}),
 * hookSel (checked names or null for untouched), setHookSel, overrides
 * (what a submit would send, token excluded), open, setOpen, remember,
 * setRemember, onReset, refusal (the server's invalid_overrides message).
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
      ${options.map((o) => html`<option key=${String(o)} value=${String(o)}>${format ? format(o) : label(o)}</option>`)}
    </select>
  </div>`;
}

function OptionsForm({ defaults, values, setValue, hooks, hookSel, setHookSel, remember, setRemember, onReset, overrides }) {
  const ch = (key) => changed(key, values, defaults);
  const bool = (key, text, hint) => html`<${Check} key=${key} name=${key} checked=${values[key]} changedMark=${ch(key)} hint=${hint} onChange=${(v) => setValue(key, v)}>${text}</${Check}>`;
  const num = (v) => Number.parseInt(v, 10);
  const extras = values.lyrics_extras;
  return html`<div class="new-options-form">
    <fieldset class="new-fieldset" data-group="quality">
      <legend class="section-title">Quality</legend>
      <${CodecPriority} list=${values.quality_priority} changedMark=${ch('quality_priority')} onChange=${(v) => setValue('quality_priority', v)} />
      ${bool('codec_alternative', 'Try the next codec when one fails', 'Off: only the first codec is tried, then AAC-LC.')}
      <div class="new-field-row">
        <${Select}
          name="alac_max_sample_rate"
          labelText="ALAC sample rate up to"
          value=${values.alac_max_sample_rate}
          options=${choices(SAMPLE_RATES, defaults.alac_max_sample_rate)}
          format=${rateLabel}
          changedMark=${ch('alac_max_sample_rate')}
          onChange=${(v) => setValue('alac_max_sample_rate', num(v))}
        />
        <${Select}
          name="alac_max_bit_depth"
          labelText="ALAC bit depth up to"
          value=${values.alac_max_bit_depth}
          options=${choices(BIT_DEPTHS, defaults.alac_max_bit_depth)}
          format=${(v) => `${v}-bit`}
          changedMark=${ch('alac_max_bit_depth')}
          onChange=${(v) => setValue('alac_max_bit_depth', num(v))}
        />
      </div>
    </fieldset>
    <fieldset class="new-fieldset" data-group="lyrics">
      <legend class="section-title">Lyrics</legend>
      ${bool('embed_lyrics', 'Embed lyrics in each file')}
      ${bool('save_lyrics_file', 'Save a lyrics file next to each track')}
      <div class="new-field-row">
        <${Select} name="lyrics_format" labelText="Lyrics format" value=${values.lyrics_format} options=${LYRICS_FORMATS} changedMark=${ch('lyrics_format')} onChange=${(v) => setValue('lyrics_format', v)} />
        <${Select} name="lyrics_type" labelText="Timing" value=${values.lyrics_type} options=${LYRICS_TYPES} changedMark=${ch('lyrics_type')} onChange=${(v) => setValue('lyrics_type', v)} />
      </div>
      <div class="new-check-group" role="group" aria-label="Extra lyric lines" data-changed=${ch('lyrics_extras') ? '' : undefined}>
        <span class="new-field-label" aria-hidden="true">Extra lines</span>
        ${LYRICS_EXTRAS.map(
          (x) => html`<${Check}
            key=${x}
            name=${`lyrics_extras.${x}`}
            checked=${extras.includes(x)}
            onChange=${(on) => setValue('lyrics_extras', on ? LYRICS_EXTRAS.filter((y) => y === x || extras.includes(y)) : extras.filter((y) => y !== x))}
          >${label(x)}</${Check}>`,
        )}
      </div>
    </fieldset>
    <fieldset class="new-fieldset" data-group="covers">
      <legend class="section-title">Covers</legend>
      ${bool('embed_cover', 'Embed the cover in each file')}
      <div class="new-field-row">
        <${Select} name="cover_size" labelText="Cover size" value=${values.cover_size} options=${choices(COVER_SIZES, defaults.cover_size)} format=${sizeLabel} changedMark=${ch('cover_size')} onChange=${(v) => setValue('cover_size', v)} />
        <${Select} name="cover_format" labelText="Cover format" value=${values.cover_format} options=${choices(COVER_FORMATS, defaults.cover_format)} changedMark=${ch('cover_format')} onChange=${(v) => setValue('cover_format', v)} />
      </div>
      ${bool('save_album_cover', 'Save the album cover as a file')}
      ${bool('save_artist_cover', 'Save the artist picture as a file')}
      ${bool('save_playlist_cover', 'Save the playlist cover as a file')}
    </fieldset>
    <fieldset class="new-fieldset" data-group="behavior">
      <legend class="section-title">Files and retries</legend>
      ${bool('force_overwrite', 'Overwrite files that already exist', 'Off: tracks already on disk are skipped.')}
      <${Select}
        name="max_attempts"
        labelText="Attempts per step"
        value=${values.max_attempts}
        options=${ATTEMPTS}
        format=${(v) => (v === 1 ? '1 (no retry)' : String(v))}
        changedMark=${ch('max_attempts')}
        onChange=${(v) => setValue('max_attempts', num(v))}
      />
    </fieldset>
    <${HooksField} hooks=${hooks} hookSel=${hookSel} setHookSel=${setHookSel} changedMark=${'hooks' in overrides} />
    <div class="new-options-foot">
      <${Check} name="remember" checked=${remember} onChange=${setRemember} hint="Kept in this browser only. The media-user-token is never stored.">Remember these options on this device</${Check}>
      <${Button} variant="ghost" size="sm" icon="rotate" className="new-options-reset" onClick=${onReset}>Reset to defaults</${Button}>
    </div>
  </div>`;
}

/** The ordered codec list: move up and down, remove (never the last), add an excluded codec. */
function CodecPriority({ list, onChange, changedMark }) {
  const excluded = CODECS.filter((c) => !list.includes(c.value));
  const info = (v) => CODECS.find((c) => c.value === v) || { value: v, label: v, hint: '' };
  return html`<div class="new-codecs" data-changed=${changedMark ? '' : undefined} data-key="quality_priority">
    <span class="new-field-label" id="new-codecs-label">Codec priority</span>
    <ol class="new-codec-list" aria-labelledby="new-codecs-label">
      ${list.map((v, i) => {
        const c = info(v);
        return html`<li key=${v} class="new-codec" data-codec=${v}>
          <span class="new-codec-rank" aria-hidden="true">${i + 1}</span>
          <span class="new-codec-name">${c.label}<span class="new-codec-hint">${c.hint}</span></span>
          <span class="new-codec-actions">
            <${Button} variant="ghost" size="sm" icon="arrow-down" className="new-codec-up" aria-label=${`Move ${c.label} up`} title="Move up" disabled=${i === 0} onClick=${() => onChange(moveItem(list, i, -1))} />
            <${Button} variant="ghost" size="sm" icon="arrow-down" aria-label=${`Move ${c.label} down`} title="Move down" disabled=${i === list.length - 1} onClick=${() => onChange(moveItem(list, i, 1))} />
            <${Button} variant="ghost" size="sm" icon="x" aria-label=${`Remove ${c.label}`} title=${list.length === 1 ? 'At least one codec is needed' : 'Remove'} disabled=${list.length === 1} onClick=${() => onChange(list.filter((x) => x !== v))} />
          </span>
        </li>`;
      })}
    </ol>
    ${excluded.length
      ? html`<div class="new-codec-add" role="group" aria-label="Add a codec">
          <span class="new-codec-add-label">Add</span>
          ${excluded.map(
            (c) => html`<${Button} key=${c.value} variant="secondary" size="sm" icon="plus" className="new-codec-add-btn" data-codec=${c.value} title=${c.hint} onClick=${() => onChange([...list, c.value])}>${c.label}</${Button}>`,
          )}
        </div>`
      : null}
    <p class="new-hint">AAC-LC is always the last fallback and is not listed.</p>
  </div>`;
}

const JOB_TYPES = { song: 'songs', album: 'albums', playlist: 'playlists', artist: 'artists', station: 'stations' };

/** Hook checkboxes from GET hooks. Untouched means every enabled entry; none checked sends []. */
function HooksField({ hooks, hookSel, setHookSel, changedMark }) {
  let body;
  if (hooks.loading && !hooks.data) body = html`<${SkeletonRows} n=${1} kind="line" />`;
  else if (hooks.error && hooks.error.status === 404) body = html`<${Unavailable} what="Hook selection" />`;
  else if (hooks.error && hooks.error.status === 503) body = html`<p class="new-hint">Hooks are not set up in this backend.</p>`;
  else if (hooks.error) body = html`<p class="new-hint">Couldn't load the hooks: ${hooks.error.message}. Every enabled hook runs.</p>`;
  else if (!hooks.data.entries.length) body = html`<p class="new-hint">No hooks are configured in hooks.yaml.</p>`;
  else if (!hooks.data.enabled) body = html`<p class="new-hint">Hooks are switched off in hooks.yaml, so none run.</p>`;
  else {
    const enabled = hooks.data.entries.filter((e) => e.enabled).map((e) => e.name);
    const sel = hookSel || enabled;
    const toggle = (name, on) => {
      const next = hooks.data.entries.map((e) => e.name).filter((n) => (n === name ? on : sel.includes(n)));
      setHookSel(next);
    };
    body = html`<div class="new-check-group" role="group" aria-label="Hooks to run">
      ${hooks.data.entries.map((e) => {
        const what = [e.type, e.events.join(', '), e.job_types.length ? e.job_types.map((t) => JOB_TYPES[t] || t).join(', ') : 'every type'].join(' · ');
        return html`<${Check}
          key=${e.name}
          name=${`hooks.${e.name}`}
          checked=${e.enabled && sel.includes(e.name)}
          disabled=${!e.enabled}
          hint=${e.enabled ? what : `Off in hooks.yaml · ${what}`}
          onChange=${(on) => toggle(e.name, on)}
        >${e.name}</${Check}>`;
      })}
      ${hookSel !== null ? html`<${Button} variant="ghost" size="sm" className="new-hooks-all" onClick=${() => setHookSel(null)}>Use every enabled hook</${Button}>` : null}
    </div>`;
  }
  return html`<fieldset class="new-fieldset" data-group="hooks" data-changed=${changedMark ? '' : undefined}>
    <legend class="section-title">Hooks</legend>
    ${body}
  </fieldset>`;
}
