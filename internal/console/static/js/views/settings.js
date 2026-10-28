// Settings (#/settings?section=): runtime configuration, the Apple Music
// account and library sync. The page head comes from app.js; this body is a
// placeholder until the settings sections replace it.

import { html } from '../ui/preact.js';
import { settingsSection, buildHash } from '../lib/route.js';
import { Card, Empty } from '../ui/components.js';

export function SettingsPage({ route }) {
  const section = settingsSection(route.params);
  return html`<div class="page page-settings" data-section-active=${section || undefined}>
    <${Card} section="settings" title="Settings" labelId="settings-title" note=${section ? `Section: ${section}` : null}>
      <${Empty} icon="settings" title="Editable settings are coming">
        The current configuration is on the <a class="link" href=${buildHash('system')}>System</a> page.
      </${Empty}>
    </${Card}>
  </div>`;
}
