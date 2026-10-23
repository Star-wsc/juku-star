(() => {
  const system = window.matchMedia('(prefers-color-scheme: dark)');
  let preference = 'system';
  try {preference = localStorage.getItem('duanju.theme') || 'system';} catch (_) {}
  if (!['system', 'light', 'dark'].includes(preference)) preference = 'system';
  function apply() {
    const theme = preference === 'system' ? (system.matches ? 'dark' : 'light') : preference;
    document.documentElement.dataset.theme = theme;
    const browserColor = document.querySelector('meta[name="theme-color"]');
    if (browserColor) browserColor.content = theme === 'dark' ? '#0e1118' : '#f3f4f8';
    document.dispatchEvent(new CustomEvent('themechange', {detail: preference}));
  }
  function set(value) {
    if (!['system', 'light', 'dark'].includes(value)) return;
    preference = value;
    try {localStorage.setItem('duanju.theme', value);} catch (_) {}
    apply();
  }
  if (system.addEventListener) system.addEventListener('change', apply);
  else system.addListener(apply);
  window.JukuTheme = {set, get: () => preference};
  apply();
})();
