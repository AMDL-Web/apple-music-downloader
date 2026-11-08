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

  // The products' screens (mocks.mjs), each with at most one control.
  // Desktop queue: Pause stops the running album, and Resume starts it again.
  for (const button of document.querySelectorAll('[data-pause]')) {
    const frame = button.closest('.dk');
    button.setAttribute('aria-pressed', 'false');
    button.addEventListener('click', () => {
      const paused = frame.classList.toggle('dk-paused');
      const label = button.textContent;
      button.textContent = button.dataset.alt;
      button.dataset.alt = label;
      button.setAttribute('aria-pressed', String(paused));
    });
  }

  // Console search: a song's download button puts it in the queue, and its
  // row says so, as the real page does.
  for (const button of document.querySelectorAll('[data-dl]')) {
    button.addEventListener('click', () => {
      if (button.classList.contains('is-done')) return;
      button.classList.add('is-done');
      button.closest('li').classList.add('is-q');
      button.setAttribute('aria-label', button.dataset.done);
      button.setAttribute('aria-disabled', 'true');
    });
  }

  // Console hooks: Run hooks switches them on and off, and the head says so.
  for (const sw of document.querySelectorAll('[data-switch]')) {
    const pill = sw.closest('.cn-hooks').querySelector('[data-pill]');
    const word = pill.querySelector('span');
    const off = word.textContent;
    sw.addEventListener('click', () => {
      const on = sw.getAttribute('aria-checked') !== 'true';
      sw.setAttribute('aria-checked', String(on));
      pill.classList.toggle('is-on', on);
      word.textContent = on ? pill.dataset.on : off;
    });
  }
})();
