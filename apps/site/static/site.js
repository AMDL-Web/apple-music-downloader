(() => {
  const root = document.documentElement;
  const store = (key, value) => {
    try {
      localStorage.setItem(key, value);
    } catch {}
  };

  const toggle = document.querySelector('[data-theme-toggle]');
  toggle?.addEventListener('click', () => {
    const current = root.getAttribute('data-theme') || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    const next = current === 'dark' ? 'light' : 'dark';
    root.setAttribute('data-theme', next);
    store('amdl-site-theme', next);
  });

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

  // Without this script both panels show, one after the other.
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
        const step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
        if (!step) return;
        event.preventDefault();
        select(tabs[(i + step + tabs.length) % tabs.length], true);
      });
    });
    list.hidden = false;
    select(tabs[0]);
  }

  // Live demos: on a narrow screen the window draws a 1280 px wide app scaled
  // to fit (site.css), and an app off screen is told so, which stops its jobs
  // moving.
  for (const frame of document.querySelectorAll('.win-body iframe')) {
    const body = frame.parentElement;
    new ResizeObserver(() => body.style.setProperty('--demo-scale', String(body.clientWidth / 1280))).observe(body);
    let offscreen = true;
    const tell = () => frame.contentWindow?.postMessage({ amdlDemoOffscreen: offscreen }, location.origin);
    new IntersectionObserver(([entry]) => {
      offscreen = !entry.isIntersecting;
      tell();
    }).observe(frame);
    frame.addEventListener('load', tell);
  }
})();
