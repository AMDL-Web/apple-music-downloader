(() => {
  const root = document.documentElement;
  const store = (key, value) => {
    try {
      if (value) localStorage.setItem(key, value);
      else localStorage.removeItem(key);
    } catch {}
  };

  // Light, dark or the system's: every switch on the page shows the choice.
  const switches = [...document.querySelectorAll('[data-theme-set]')];
  const showTheme = () => {
    const current = root.getAttribute('data-theme') || 'system';
    for (const b of switches) {
      const on = b.dataset.themeSet === current;
      b.setAttribute('aria-checked', String(on));
      b.tabIndex = on ? 0 : -1;
    }
  };
  showTheme();
  for (const b of switches) {
    b.addEventListener('click', () => {
      const value = b.dataset.themeSet;
      if (value === 'system') root.removeAttribute('data-theme');
      else root.setAttribute('data-theme', value);
      store('amdl-site-theme', value === 'system' ? '' : value);
      showTheme();
    });
  }
  // Arrow keys move along a radio group, as they do in a native one.
  for (const group of document.querySelectorAll('[role="radiogroup"]')) {
    const items = [...group.querySelectorAll('[role="radio"]')];
    group.addEventListener('keydown', (event) => {
      const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[event.key];
      if (!step) return;
      event.preventDefault();
      const next = items[(items.indexOf(document.activeElement) + step + items.length) % items.length];
      next.click();
      next.focus();
    });
  }

  for (const button of document.querySelectorAll('[data-copy]')) {
    const label = button.getAttribute('aria-label');
    let timer;
    button.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(button.dataset.copy);
      } catch {
        return;
      }
      button.classList.add('is-done');
      button.setAttribute('aria-label', button.dataset.copied);
      clearTimeout(timer);
      timer = setTimeout(() => {
        button.classList.remove('is-done');
        button.setAttribute('aria-label', label);
      }, 1600);
    });
  }

  // The page draws each tab list with its first tab open (blocks.mjs#tabs).
  for (const list of document.querySelectorAll('[role="tablist"]')) {
    const tabs = [...list.querySelectorAll('[role="tab"]')];
    const select = (tab, focus) => {
      for (const t of tabs) {
        const on = t === tab;
        t.setAttribute('aria-selected', String(on));
        t.tabIndex = on ? 0 : -1;
        document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
      }
      if (focus) tab.focus();
    };
    tabs.forEach((tab, i) => {
      tab.addEventListener('click', () => select(tab));
      tab.addEventListener('keydown', (event) => {
        const step = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: 1, ArrowUp: -1 }[event.key];
        if (!step) return;
        event.preventDefault();
        select(tabs[(i + step + tabs.length) % tabs.length], true);
      });
    });
  }

  // The product views (mocks.mjs). Search: Download puts a result in the
  // queue. Queue: the running album moves through the stages while its view
  // is open, and Retry queues the failed one again. Library: Download missing
  // fetches the missing file and the record becomes complete.
  for (const button of document.querySelectorAll('[data-dl]')) {
    const queued = button.closest('[data-queued]').dataset.queued;
    button.addEventListener('click', () => {
      if (button.classList.contains('is-queued')) return;
      button.classList.add('is-queued');
      button.querySelector('span').textContent = queued;
      button.setAttribute('aria-label', queued);
      button.setAttribute('aria-disabled', 'true');
    });
  }

  const retry = document.querySelector('[data-retry]');
  retry?.addEventListener('click', () => {
    const row = retry.closest('.q-row');
    const pill = row.querySelector('.pill');
    pill.className = 'pill pill-wait';
    pill.querySelector('span').textContent = retry.dataset.queued;
    retry.remove();
  });

  const still = matchMedia('(prefers-reduced-motion: reduce)');
  const run = document.querySelector('[data-run]');
  if (run) {
    const stages = JSON.parse(run.dataset.stages);
    const order = Object.keys(stages);
    const pill = run.querySelector('[data-stage]');
    const pillText = run.querySelector('[data-stage-text]');
    const trackText = run.querySelector('[data-track-text]');
    const bar = run.querySelector('[data-bar]');
    const TOTAL = 12;
    let track = 8;
    let stage = 1;
    let timer = 0;
    const draw = () => {
      pill.dataset.stage = order[stage];
      pillText.textContent = stages[order[stage]];
      trackText.textContent = run.dataset.track.replace('{n}', track).replace('{total}', TOTAL);
      bar.style.setProperty('--p', `${Math.round(((track - 1 + stage / order.length) / TOTAL) * 100)}%`);
    };
    const step = () => {
      stage++;
      if (stage === order.length) {
        stage = 0;
        track = track === TOTAL ? 7 : track + 1;
      }
      draw();
    };
    const panel = run.closest('[role="tabpanel"]');
    const sync = (visible) => {
      clearInterval(timer);
      if (visible && !panel.hidden && !still.matches) timer = setInterval(step, 1100);
    };
    let onScreen = false;
    new IntersectionObserver((entries) => {
      onScreen = entries.some((e) => e.isIntersecting);
      sync(onScreen);
    }).observe(panel.parentElement);
    new MutationObserver(() => sync(onScreen)).observe(panel, { attributes: true, attributeFilter: ['hidden'] });
  }

  const fix = document.querySelector('[data-fix]');
  fix?.addEventListener('click', () => {
    const rec = fix.closest('[data-record]');
    const cell = rec.querySelector('[data-missing]');
    const path = rec.querySelector('[data-path]');
    const badges = rec.closest('.lib').querySelectorAll('[data-badge]');
    fix.disabled = true;
    const done = () => {
      cell.className = 'slot is-done';
      cell.querySelector('svg').outerHTML = rec.querySelector('.slot.is-done svg').outerHTML;
      cell.querySelector('.slot-word').textContent = rec.dataset.done;
      for (const badge of badges) {
        badge.className = 'badge badge-ok';
        badge.textContent = rec.dataset.complete;
      }
      path.textContent = path.dataset.fixed;
    };
    if (still.matches) done();
    else {
      cell.className = 'slot is-busy';
      setTimeout(done, 900);
    }
  });
})();
