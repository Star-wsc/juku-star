import { $, element, empty, button, setMessage, api, post, valueText, firstNonEmpty, dramaTitle, watchLabel, historySuffix, normalizeSource, sourceKey, sourceLabel, categoryName, episodeCount, coverURL, tagsText, dramaSearchText, rebuildOptions, number, formatBytes, formatTime, statusText, releaseText, phaseText, progressText, progressBar, episodeLabel, groupStats } from './ui-core.js';

export function createSettings(app) {
  let config = {};
function refreshConfigFields(){ $('downloadLocation').textContent=config.outputDir||'尚未设置';$('downloadLocation').title=config.outputDir||''; $('downloadDirectory').value=config.outputDirSetting||config.outputDir||'./短剧下载';$('downloadDirectoryHint').textContent=(config.restartRequired?'已保存新目录，重启后生效。':'')+'选择程序所在电脑的文件夹，或输入相对/绝对路径。重启后新合集使用新目录，原任务和文件保留。';$('downloadConcurrency').value=config.concurrency||2;$('requestConcurrency').value=config.requestConcurrency||2;$('requestInterval').value=config.requestIntervalMs||500;$('proxyMode').value=config.proxyMode||'auto';$('proxyURL').value=config.proxyURL||'';$('proxyUsername').value='';$('proxyPassword').value='';$('proxyPassword').placeholder=config.proxyHasAuth?'已保存，留空保留认证':'';updateProxyFields();$('configText').textContent='当前输出：'+(config.outputDir||'')+' · 下载并发 '+(config.concurrency||2)+' · '+(config.network||'')+' · FFmpeg '+(config.ffmpeg||''); }

function updateProxyFields(){const disabled=$('proxyMode').value!=='manual';for(const name of ['proxyURL','proxyUsername','proxyPassword'])$(name).disabled=disabled;}

async function loadConfig(){try{config=await api('/api/ui/config');refreshConfigFields();await loadPlaybackSettings();}catch(error){$('configText').textContent='配置读取失败：'+error.message;}}

async function loadPlaybackSettings(){
  try{
    const playback=await api('/api/ui/playback/settings');
    $('playbackOriginalEnabled').value=playback.enabled?'1':'0';
    $('playbackDirectSources').value=(playback.directSources||[]).join(',');
    $('playbackMaxSessions').value=playback.maxSessions||4;
    $('playbackCacheMB').value=playback.cacheMB||512;
    $('playbackBackground').value=playback.backgroundPlayback?'1':'0';
  }catch(error){/* 1.0 后端无此接口时忽略 */}
  try{
    const emby=await api('/api/ui/admin/emby');
    $('embyBaseURL').value=emby.baseUrl||'';
    $('embyAPIKey').value=emby.apiKey||'';
    $('embyAutoSync').value=emby.autoSync?'1':'0';
    $('playbackGroupBySource').value=emby.groupBySource?'1':'0';
  }catch(error){/* 未登录管理员时忽略 */}
}

async function savePlaybackSettings(){
  const button=$('savePlaybackSettingsBtn');
  button.disabled=true;
  try{
    const playback={
      enabled:$('playbackOriginalEnabled').value==='1',
      directSources:$('playbackDirectSources').value.split(',').map(v=>v.trim()).filter(Boolean),
      maxSessions:Number($('playbackMaxSessions').value)||4,
      cacheMB:Number($('playbackCacheMB').value)||512,
      backgroundPlayback:$('playbackBackground').value==='1',
    };
    await post('/api/ui/admin/playback',playback);
    const emby={
      enabled:Boolean($('embyBaseURL').value.trim()),
      baseUrl:$('embyBaseURL').value.trim(),
      apiKey:$('embyAPIKey').value.trim(),
      autoSync:$('embyAutoSync').value==='1',
      groupBySource:$('playbackGroupBySource').value==='1',
    };
    await post('/api/ui/admin/emby',emby);
    $('settingsStatus').textContent='播放与 Emby 设置已保存';
  }catch(error){$('settingsStatus').textContent='播放设置保存失败：'+error.message;}
  finally{button.disabled=false;}
}

async function syncEmby(){
  const button=$('embySyncBtn');
  button.disabled=true;
  $('settingsStatus').textContent='正在导出并通知 Emby…';
  try{
    const result=await post('/api/ui/admin/emby/sync',{notify:true});
    $('settingsStatus').textContent='Emby 同步完成：导出 '+(result.exported||0)+' 部'+(result.notified?'，已通知刷新':'');
  }catch(error){$('settingsStatus').textContent='Emby 同步失败：'+error.message;}
  finally{button.disabled=false;}
}

async function saveSettings(){
    const settings={outputDir:$('downloadDirectory').value.trim(),concurrency:number($('downloadConcurrency').value),requestConcurrency:number($('requestConcurrency').value),requestIntervalMs:number($('requestInterval').value)};
    try{
      const mode=$('proxyMode').value;if(mode!=='manual')settings.proxyURL=mode;else{const raw=$('proxyURL').value.trim();const username=$('proxyUsername').value;const password=$('proxyPassword').value;if(!(config.proxyHasAuth&&config.proxyMode==='manual'&&raw===config.proxyURL&&!username&&!password)){const endpoint=new URL(raw);if(!['http:','https:','socks5:','socks5h:'].includes(endpoint.protocol))throw new Error('仅支持 HTTP/HTTPS/SOCKS5 代理');if(username||password){endpoint.username=username;endpoint.password=password;}settings.proxyURL=endpoint.toString();}}
      $('saveSettingsBtn').disabled=true;config=await post('/api/ui/config',settings);refreshConfigFields();$('settingsStatus').textContent=config.restartRequired?'设置已保存；下载目录重启后生效，当前任务保持原路径':'设置已保存并应用';
    }catch(error){$('settingsStatus').textContent='保存失败：'+error.message;}finally{$('saveSettingsBtn').disabled=false;}
  }

async function checkNetwork(){ $('checkNetworkBtn').disabled=true;$('settingsStatus').textContent='正在检测已保存的网络配置、各站入口和黄果实际媒体，最多约一分钟…';try{const result=await post('/api/ui/network/check',{});$('settingsStatus').textContent=(result.data||[]).map(item=>item.name+'：'+(item.error||[item.status?'HTTP '+item.status:'',item.detail||''].filter(Boolean).join(' · '))).join('；');}catch(error){$('settingsStatus').textContent='检测失败：'+error.message;}finally{$('checkNetworkBtn').disabled=false;} }

async function chooseDownloadDirectory(){const button=$('browseDirectoryBtn');if(button.disabled)return;button.disabled=true;$('settingsStatus').textContent='请在系统窗口中选择下载文件夹…';try{const result=await post('/api/ui/directory/pick',{initialPath:$('downloadDirectory').value});if(result.canceled){$('settingsStatus').textContent='已取消选择，原目录未修改';return;}$('downloadDirectory').value=result.selectionPath||result.path;$('settingsStatus').textContent='已选择文件夹，请点击“保存并应用”';}catch(error){$('settingsStatus').textContent='选择文件夹失败：'+error.message;}finally{button.disabled=false;}}

async function importLegacyRecords() {
  const control = $('legacyImportBtn');
  control.disabled = true;
  $('viewerImportStatus').textContent = '正在接回旧版记录…';
  try {
    await post('/api/ui/viewer/legacy', {code: $('legacyImportCode').value.trim()});
    $('legacyImportCode').value = '';
    $('legacyImportSection').hidden = true;
    app.viewer.legacyAvailable = false;
    await Promise.all([window.JukuHistory.refresh(), app.following.refresh()]);
    $('viewerImportStatus').textContent = app.viewer?.account ? '旧版记录已接入当前账号，原始文件仍保留。' : '旧版记录已接回当前浏览器，原始文件仍保留。';
  } catch (error) {
    $('viewerImportStatus').textContent = error.message;
  } finally {
    control.disabled = false;
  }
}

function init() {
  const admin = Boolean(app.viewer?.account?.admin && !app.viewer.account.requirePasswordChange);
  $('serverSettings').hidden = !admin;
  $('serverSettingsNotice').hidden = admin;
  $('downloadLocationBtn').hidden = !admin;
  if (!admin) $('downloadLocation').textContent = '服务端下载目录';
  $('legacyImportSection').hidden = !app.viewer?.legacyAvailable;
  $('viewerImportStatus').textContent = app.viewer?.legacyError || '';
  $('legacyImportBtn').addEventListener('click', importLegacyRecords);
  $('browseDirectoryBtn').addEventListener('click', chooseDownloadDirectory);
  $('proxyMode').addEventListener('change', updateProxyFields);
  $('saveSettingsBtn').addEventListener('click', saveSettings);
  $('savePlaybackSettingsBtn').addEventListener('click', savePlaybackSettings);
  $('embySyncBtn').addEventListener('click', syncEmby);
  $('checkNetworkBtn').addEventListener('click', checkNetwork);
  $('openSettingsBtn').addEventListener('click', () => window.JukuDialogs.open('settingsPanel'));
  $('closeSettingsBtn').addEventListener('click', () => $('settingsPanel').close());
  $('themeSelect').value = window.JukuTheme.get();
  $('themeSelect').addEventListener('change', () => window.JukuTheme.set($('themeSelect').value));
  document.addEventListener('themechange', event => {$('themeSelect').value = event.detail;});
  return admin ? loadConfig() : Promise.resolve();
}
return {init, config: () => config};

}
