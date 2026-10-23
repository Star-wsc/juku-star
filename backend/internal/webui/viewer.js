let identity = '', sources = '', onlineOnly = 'false';
let changed = false;
const changeKey = 'juku.account.change';

export function announceViewerChange() {
  try { localStorage.setItem(changeKey, Date.now() + '-' + Math.random()); } catch (_) {}
}

window.addEventListener('storage', event => {
  if (event.key === changeKey && identity && !changed) {
    changed = true;
    window.dispatchEvent(new Event('jukuviewerchange'));
  }
});

export function viewerHeaders() {
  return identity ? {'X-Juku-Viewer': identity, 'X-Juku-Sources': sources, 'X-Juku-Online-Only': onlineOnly} : {};
}

export function checkViewerResponse(result) {
  if (!identity || changed || !['viewer_required', 'viewer_changed', 'login_required', 'password_change_required', 'sources_changed', 'permissions_changed'].includes(result?.code)) return;
  changed = true;
  window.dispatchEvent(new Event('jukuviewerchange'));
}

export async function initializeViewer() {
  async function read(path) {
    const response = await fetch(path, {credentials: 'same-origin', cache: 'no-store', headers: {'Accept': 'application/json'}});
    let result;
    try {result = await response.json();} catch (_) {throw new Error('无法建立独立观看记录，请确认已启动新版程序');}
    if (!response.ok) throw new Error(result.error || '无法建立独立观看记录');
    return result;
  }
  let result = await read('/api/ui/viewer');
  if (!result.ready) result = await read('/api/ui/viewer?confirm=1');
  if (!result.ready || !/^[a-f0-9]{64}$/.test(result.id)) throw new Error('无法确认浏览器身份，请刷新页面重试');
  identity = result.id;
  sources = (result.sources || []).join(',');
  onlineOnly = String(result.onlineOnly === true);
  changed = false;
  return result;
}

window.JukuViewer = {headers: viewerHeaders, checkResponse: checkViewerResponse};
