(() => {
  const key = 'juku.vip.show';
  let shown = false, enabled = false;
  try {shown = localStorage.getItem(key) === 'true';} catch (_) {}
  const isVIP = drama => drama?.vip === true;
  const visible = drama => shown || !isVIP(drama);
  function refresh() {
    for (const button of document.querySelectorAll('[data-vip-filter]')) {
      button.textContent = shown ? '显示 VIP' : '隐藏 VIP';
      button.setAttribute('aria-pressed', String(shown));
      button.title = shown ? '当前显示 VIP，点击切换为隐藏；VIP 分集仅能试看' : '当前隐藏已识别的 VIP，点击切换为显示；未识别内容会分批检查';
      if (!enabled) button.hidden = true;
    }
  }
  function changed(value, interactive = false) {
    shown = Boolean(value);
    refresh();
    window.dispatchEvent(new CustomEvent('jukuvipfilterchange', {detail: {interactive}}));
  }
  function toggle() {
    if (!enabled) return;
    try {localStorage.setItem(key, String(!shown));} catch (_) {}
    changed(!shown, true);
  }
  function init(allowed) {
    enabled = Boolean(allowed);
    for (const button of document.querySelectorAll('[data-vip-filter]')) {
      button.hidden = !enabled;
      button.addEventListener('click', toggle);
    }
    refresh();
  }
  window.addEventListener('storage', event => {if (event.key === key) changed(event.newValue === 'true');});
  function summary(dramas) {
    const rows = dramas.filter(drama => drama.source === 'huangdou' || drama.id?.startsWith('huangdou:'));
    if (!rows.length) return '';
    const paid = rows.filter(isVIP).length, unknown = rows.filter(drama => drama.vip == null).length;
    return (shown ? '已显示 ' : '已隐藏 ') + paid + ' 部 VIP' + (unknown ? ' · ' + unknown + ' 部 VIP 状态待识别，暂保留显示' : '');
  }
  window.JukuVIP = {isVIP, visible, summary, init, get shown() {return shown;}};
})();
