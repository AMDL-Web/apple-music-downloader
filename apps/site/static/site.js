(() => {
  const root = document.documentElement;
  const store = (key, value) => {
    try {
      if (value) localStorage.setItem(key, value);
      else localStorage.removeItem(key);
    } catch {}
  };

  // Light, dark or the system's: both theme switches (header and menu) show
  // the choice.
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
  // Arrow keys move within the radio group that has focus, as in a native one.
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
        const to = { Home: 0, End: tabs.length - 1 }[event.key];
        if (!step && to === undefined) return;
        event.preventDefault();
        select(tabs[to ?? (i + step + tabs.length) % tabs.length], true);
      });
    });
    // The stage arrows in each panel select the previous or next tab,
    // wrapping, and move focus to it.
    const widget = list.closest('.tabs');
    for (const arrow of widget.querySelectorAll('.stage-arrow')) {
      arrow.addEventListener('click', () => {
        const at = tabs.findIndex((t) => t.getAttribute('aria-selected') === 'true');
        const step = arrow.classList.contains('prev') ? -1 : 1;
        select(tabs[(at + step + tabs.length) % tabs.length], true);
      });
    }
  }

  // The Sections menu: Escape closes it and returns focus to its button; a
  // link or a click elsewhere closes it too.
  for (const menu of document.querySelectorAll('details.sections')) {
    const summary = menu.querySelector('summary');
    menu.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape' || !menu.open) return;
      menu.open = false;
      summary.focus();
    });
    for (const link of menu.querySelectorAll('a[href^="#"]')) link.addEventListener('click', () => (menu.open = false));
    document.addEventListener('click', (event) => {
      if (menu.open && !menu.contains(event.target)) menu.open = false;
    });
  }
})();
