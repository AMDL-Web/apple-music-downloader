// Puts the stored theme on <html> before the first paint. A classic script
// in <head> blocks rendering until it has run; main.js is a module, which runs
// only after the page has been painted once in the system's theme. The key and
// values are lib/prefs.js's THEME_KEY and THEMES (jstest/prefs.test.mjs).
(function () {
  try {
    var theme = window.localStorage.getItem('amdl.console.theme');
    if (theme === 'light' || theme === 'dark') document.documentElement.setAttribute('data-theme', theme);
  } catch (e) {
    // Storage blocked: the system theme, as main.js will decide too.
  }
})();
