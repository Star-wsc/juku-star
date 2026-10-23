import { api, post, sourceLabel, normalizeSource, setMessage } from './ui-core.js';
import { createShell } from './shell.js';
import { createLibrary } from './library.js';
import { createDownloads } from './downloads.js';
import { createSettings } from './settings.js';
import { createFollowing } from './following.js';
import { createDetails } from './details.js';
import { initializeViewer } from './viewer.js';
import { createAccount } from './account.js';
import { createUsers } from './users.js';

const app = {api, post};
app.play = (id, title) => {
  if (window.JukuHistory.get(id)) window.dramaPlayer.openHistory(id, title);
  else window.dramaPlayer.open(id, title);
  app.following.acknowledge(id);
  app.library.refreshDrama(id);
};
app.shell = createShell(app);
app.library = createLibrary(app);
app.downloads = createDownloads(app);
app.settings = createSettings(app);
app.following = createFollowing(app);
app.details = createDetails(app);
app.account = createAccount(app);
app.users = createUsers(app);

app.shell.init();
window.addEventListener('jukuviewerchange', () => {
  document.getElementById('viewerNotice').textContent = '账号权限已变化，正在重新加载…';
  document.getElementById('viewerNotice').hidden = false;
  window.location.reload();
});

async function initialize() {
  try {
    app.viewer = await initializeViewer();
  } catch (error) {
    const notice = document.getElementById('viewerNotice');
    const retry = document.createElement('button');
    retry.type = 'button';
    retry.className = 'secondary';
    retry.textContent = '重试';
    retry.addEventListener('click', () => window.location.reload());
    notice.replaceChildren(document.createTextNode(error.message), retry);
    notice.hidden = false;
    return;
  }
if (app.account.init()) return;
app.users.init();
app.shell.access();
window.JukuVIP.init(app.viewer?.sources?.includes('huangdou') !== false);
window.JukuHistory.init({
  api, post,
  sourceLabel: value => sourceLabel(normalizeSource(value) || value),
  onChanged: () => {app.library.refreshFollowing(); app.following.render(); app.details.refresh(); app.downloads.render();},
  onError: message => setMessage(message, true),
  play: app.play
});
window.JukuRankings.init({
  api, post,
  getSource: () => document.getElementById('sourceSelect').value,
  getDrama: id => app.library.get(id),
  sourceLabel,
  onLibraryChanged: () => app.library.refresh(),
  canDownload: () => !app.viewer?.onlineOnly,
  onDownloadsChanged: app.downloads.refresh,
  getDownloadQuality: app.downloads.quality,
  play: app.play
});
Promise.allSettled([app.settings.init(), app.library.init(), app.downloads.init(), app.following.init()]).then(results => {
  const failure = results.find(result => result.status === 'rejected');
  if (failure) setMessage('部分功能未能初始化：' + (failure.reason?.message || '请刷新页面重试'), true);
  document.documentElement.dataset.ready = 'true';
});

}

initialize();
