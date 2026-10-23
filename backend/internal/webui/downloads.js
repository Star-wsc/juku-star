import { reconcileChildren, withFocus, readPreference, savePreference } from './ui-core.js';
import { $, element, empty, button, setMessage, api, post, valueText, firstNonEmpty, dramaTitle, watchLabel, historySuffix, normalizeSource, sourceKey, sourceLabel, categoryName, episodeCount, coverURL, tagsText, dramaSearchText, rebuildOptions, number, formatBytes, formatTime, statusText, releaseText, phaseText, progressText, progressBar, episodeLabel, groupStats } from './ui-core.js';

export function createDownloads(app) {
  let tasks = [], visibleGroups = [], mergeStates = {}, taskRequest = false, ffmpegStatus = '';
  const selectedTasks = new Set(), openGroups = new Set(), groupsEl = $('groups');
  let pollTimer, pollPromise, batch = false, taskSignature = '', ffmpegSignature = '';
  let hasActiveTasks = false, polling = false, scheduledDelay = 0;
  const qualityOptions = [0, 2160, 1440, 1080, 720, 540, 480, 360];
  const storedQuality = number(readPreference('downloadQuality', 0));
  let downloadQuality = qualityOptions.includes(storedQuality) ? storedQuality : 0;
  const qualityLabel = value => value ? value + 'p 优先' : '最高可用';

  function setupQualitySelect(select) {
    select.dataset.downloadQuality = '';
    for (const quality of qualityOptions) {
      const option = element('option', '', qualityLabel(quality));
      option.value = String(quality);
      select.appendChild(option);
    }
    select.value = String(downloadQuality);
    select.title = '用于新加入的下载任务；缺少所选档位时取不高于该档位的最高画质，全部更高时取最低可用画质。保留源文件，不压缩。';
    select.addEventListener('change', () => {
      downloadQuality = number(select.value);
      savePreference('downloadQuality', downloadQuality);
      document.querySelectorAll('[data-download-quality]').forEach(node => {node.value = String(downloadQuality);});
    });
  }

  function qualityControl() {
    const label = element('label', 'download-quality', '下载画质');
    const select = element('select');
    select.setAttribute('aria-label', '本次下载清晰度');
    select.dataset.focusKey = 'detail-download-quality';
    setupQualitySelect(select);
    label.appendChild(select);
    return label;
  }
function buildGroups(){const byID=new Map();tasks.forEach(task=>{const id=task.dramaId||task.dramaTitle||task.id;let group=byID.get(id);if(!group){group={id,title:task.dramaTitle||'短剧',tasks:[],release:'unknown'};byID.set(id,group);}group.tasks.push(task);if(task.releaseStatus)group.release=task.releaseStatus;});return Array.from(byID.values());}

function groupMatches(group){const keyword=$('taskSearch').value.trim().toLowerCase();if(keyword&&!group.title.toLowerCase().includes(keyword))return false;const release=$('releaseStatus').value;if(release&&group.release!==release)return false;const status=$('taskStatus').value;const stats=groupStats(group);if(status==='completed')return stats.success===group.tasks.length;if(status==='unfinished')return stats.success!==group.tasks.length;if(status==='failed')return stats.failed>0;if(status==='running')return stats.running+stats.parsing+stats.queued>0;if(status==='paused')return stats.paused>0;if(status==='canceled')return stats.canceled>0;return true;}

function selectGroup(group, checked){group.tasks.forEach(task=>{if(checked)selectedTasks.add(task.id);else selectedTasks.delete(task.id);});renderTasks(tasks);}

function selectedTaskList(){return tasks.filter(task=>selectedTasks.has(task.id));}

function selectedGroupIDs(){return Array.from(new Set(selectedTaskList().map(task=>task.dramaId)));}

function updateTaskSelection(){const visible=new Set(visibleGroups.flatMap(group=>group.tasks.map(task=>task.id)));const hidden=selectedTaskList().filter(task=>!visible.has(task.id)).length;$('taskSelection').textContent='已选 '+selectedTasks.size+' 集 / '+selectedGroupIDs().length+' 部'+(hidden?'（隐藏 '+hidden+' 集）':'');for(const id of ['pauseSelectedBtn','resumeSelectedBtn','cancelSelectedBtn','retrySelectedBtn','updateSelectedBtn','mergeSelectedBtn','clearTasksBtn'])$(id).disabled=selectedTasks.size===0;}

function renderTasks(next) {
  if (app.viewer?.onlineOnly) return;
  if (tasks !== next) taskSignature = '';
  tasks = next;
  if (selectedTasks.size) {
    const existing = new Set(tasks.map(task => task.id));
    selectedTasks.forEach(id => {if (!existing.has(id)) selectedTasks.delete(id);});
  }
  let active = 0, completed = 0, written = 0;
  for (const task of tasks) {
    if (['running', 'queued', 'parsing'].includes(task.status)) active++;
    if (task.status === 'success') completed++;
    written += number(task.downloadedBytes);
  }
  const setText = (node, value) => {if (node.textContent !== String(value)) node.textContent = value;};
  setText($('downloadActiveCount'), active);
  setText($('downloadCompleteCount'), completed);
  setText($('downloadWrittenBytes'), formatBytes(written));
  document.querySelectorAll('[data-download-count]').forEach(node => {setText(node, active); if (node.hidden !== !active) node.hidden = !active;});
  hasActiveTasks = active > 0;
  const showActivity = !$('activityPanel').hidden && !$('libraryPage').hidden;
  if ($('downloadsPage').hidden && !showActivity) return;
  const all = buildGroups();
  if (!$('downloadsPage').hidden) {
    visibleGroups = all.filter(groupMatches);
    setText($('taskCount'), all.length ? visibleGroups.length + ' 部' : '');
    const previous = new Map(Array.from(groupsEl.querySelectorAll('.group'), node => [node.dataset.dramaId, node]));
    withFocus(groupsEl, () => {
      const nodes = [];
      for (const group of visibleGroups) {
        const old = previous.get(group.id);
        const key = JSON.stringify([group, mergeStates[group.id], openGroups.has(group.id), batch, group.tasks.map(task => selectedTasks.has(task.id)), historySuffix(group.id)]);
        if (old?.renderKey === key) {nodes.push(old); continue;}
        const menuOpen = old?.querySelector('.action-menu')?.open;
        const node = renderGroup(group);
        node.renderKey = key;
        if (menuOpen) node.querySelector('.action-menu').open = true;
        nodes.push(node);
      }
      if (!visibleGroups.length) {
        const empty = element('div', 'empty');
        empty.append(element('strong', '', tasks.length ? '没有匹配的下载合集' : '还没有下载任务'), element('p', '', tasks.length ? '试试其他筛选条件。' : '在剧集详情加入下载，也可以在剧库批量选择。'));
        if (!tasks.length) empty.appendChild(button('去剧库看看', () => window.JukuDialogs.navigate('library'), false, 'secondary'));
        nodes.push(empty);
      }
      reconcileChildren(groupsEl, nodes);
    });
    updateTaskSelection();
  }
  if (showActivity) renderActivity(all);

}

function renderGroup(group) {
  const stats = groupStats(group);
  const wrap = element('article', 'group' + (openGroups.has(group.id) ? ' open' : ''));
  wrap.dataset.dramaId = group.id;
  const head = element('div', 'group-head');
  const heading = element('div', 'group-heading');
  const checkbox = element('input');
  checkbox.type = 'checkbox';
  checkbox.setAttribute('aria-label', '选择合集 ' + group.title);
  checkbox.dataset.focusKey = 'group-select-' + group.id;
  const selectedCount = group.tasks.filter(task => selectedTasks.has(task.id)).length;
  checkbox.checked = selectedCount === group.tasks.length;
  checkbox.indeterminate = selectedCount > 0 && !checkbox.checked;
  checkbox.addEventListener('change', () => selectGroup(group, checkbox.checked));
  const info = element('div');
  info.appendChild(element('h2', 'group-title', group.title));
  const statuses = [releaseText(group.release), '共 ' + group.tasks.length + ' 集', '完成 ' + stats.success];
  const qualities = [...new Set(group.tasks.map(task => number(task.downloadQuality)))];
  statuses.push('画质：' + (qualities.length === 1 ? qualityLabel(qualities[0]) : '按分集设置'));
  for (const [key, label] of [['running','下载中'],['queued','排队'],['parsing','解析中'],['paused','暂停'],['failed','失败'],['canceled','已取消']]) if (stats[key]) statuses.push(label + ' ' + stats[key]);
  const meta = element('p', 'small', statuses.join(' · '));
  meta.appendChild(element('span', 'group-watch-progress', historySuffix(group.id)));
  info.appendChild(meta);
  const selection = element('label', 'selection-target');
  selection.appendChild(checkbox);
  heading.append(selection, info);
  head.append(heading, progressBar(stats.percent), element('p', 'small', stats.percent + '% · 已写入 ' + formatBytes(stats.bytes) + (stats.speed > 0 ? ' · ' + formatBytes(stats.speed) + '/s' : '')));
  const merge = mergeStates[group.id];
  if (merge) head.appendChild(element('p', merge.error ? 'error notice' : 'small notice', '合并：' + ({running:'进行中', success:'完成', failed:'失败'}[merge.status] || merge.status) + ' ' + (merge.progress || 0) + '%' + (merge.detail ? ' · ' + merge.detail : '') + (merge.error ? ' · ' + merge.error : '')));
  const actions = element('div', 'group-actions');
  function action(label, key, callback, disabled = false, className = 'secondary') {
    const node = button(label, callback, disabled, className);
    node.dataset.focusKey = group.id + '-' + key;
    return node;
  }
  const playable = group.tasks.filter(task => task.playable).sort((left, right) => number(left.index) - number(right.index))[0];
  actions.appendChild(action(watchLabel(group.id, '播放'), 'play', () => window.dramaPlayer.openCollection(playable.id, group.title, true), !playable, 'secondary collection-play-button'));
  const toggle = action(openGroups.has(group.id) ? '收起分集' : '查看分集', 'episodes', () => {
    if (openGroups.has(group.id)) openGroups.delete(group.id); else openGroups.add(group.id);
    renderTasks(tasks);
  });
  toggle.setAttribute('aria-expanded', String(openGroups.has(group.id)));
  actions.appendChild(toggle);
  if (stats.running + stats.queued + stats.parsing) actions.appendChild(action('暂停', 'pause', () => taskAction('pause', [], [group.id])));
  if (stats.paused) actions.appendChild(action('继续', 'resume', () => taskAction('resume', [], [group.id])));
  if (stats.failed + stats.canceled) actions.appendChild(action('重试失败', 'retry', () => taskAction('retry', [], [group.id])));
  const menu = element('details', 'action-menu');
  const summary = element('summary', '', '更多操作');
  summary.dataset.focusKey = group.id + '-more';
  const popover = element('div', 'action-popover');
  popover.append(action('更新本剧', 'update', () => updateGroups([group.id])), action('合并已完成分集', 'merge', () => mergeGroups([group.id]), !stats.success), action('取消本剧下载', 'cancel', () => taskAction('cancel', [], [group.id]), !stats.running && !stats.queued && !stats.parsing && !stats.paused));
  menu.append(summary, popover);
  actions.appendChild(menu);
  head.appendChild(actions);
  const list = element('div', 'episode-list');
  if (openGroups.has(group.id)) for (const task of group.tasks) {
    const episode = renderEpisode(task);
    episode.querySelectorAll('button,input').forEach((control, index) => control.dataset.focusKey = 'task-' + task.id + '-' + index);
    list.appendChild(episode);
  }
  wrap.append(head, list);
  return wrap;
}

function renderEpisode(task){
    const row=element('div','episode');const checkbox=element('input');checkbox.type='checkbox';checkbox.checked=selectedTasks.has(task.id);checkbox.setAttribute('aria-label','选择 '+episodeLabel(task));checkbox.addEventListener('change',()=>{if(checkbox.checked)selectedTasks.add(task.id);else selectedTasks.delete(task.id);renderTasks(tasks);});const selection=element('label','selection-target');selection.appendChild(checkbox);row.appendChild(selection);const content=element('div','episode-main');const heading=element('div','episode-heading');heading.appendChild(element('span','pill '+task.status,phaseText(task)));heading.appendChild(element('strong','',task.status==='parsing'||task.title==='章节获取失败'?task.title:episodeLabel(task)));content.appendChild(heading);content.appendChild(element('div','small episode-path',task.path||''));if(task.error)content.appendChild(element('div','error',task.error));content.appendChild(progressBar(task.progress));content.appendChild(element('div','small',progressText(task)+' · 已写入 '+formatBytes(task.downloadedBytes)+(number(task.totalBytes)?' / '+formatBytes(task.totalBytes):'')+' · '+(number(task.speedBytesPerSecond)>0?formatBytes(task.speedBytesPerSecond)+'/s':'速度 —')));content.appendChild(element('div','small','已用 '+formatTime(task.elapsedSeconds)+' · 剩余 '+formatTime(task.remainingSeconds)+' · 尝试 '+(task.attempt||0)));
    const actions=element('div','episode-actions');if(task.playable)actions.appendChild(button('播放',()=>window.dramaPlayer.openCollection(task.id,task.dramaTitle),false,'secondary episode-play-button'));if(['running','queued','parsing'].includes(task.status))actions.appendChild(button('暂停',()=>taskAction('pause',[task.id]),task.pauseRequested));if(task.status==='paused')actions.appendChild(button('继续',()=>taskAction('resume',[task.id])));if(['running','queued','parsing','paused'].includes(task.status))actions.appendChild(button('取消',()=>taskAction('cancel',[task.id]),task.cancelRequested));if(['failed','canceled'].includes(task.status))actions.appendChild(button('重试',()=>taskAction('retry',[task.id])));content.appendChild(actions);row.appendChild(content);return row;
  }

async function retryFFmpeg(){try{await post('/api/ui/ffmpeg',{});setMessage('已重新检查 FFmpeg，准备进度会自动更新');await pollTasks();}catch(error){setMessage(error.message,true);}}

function renderFFmpeg(state){if(!state)return;const retryCovers=ffmpegStatus&&ffmpegStatus!=='ready'&&state.status==='ready';ffmpegStatus=state.status;let text=state.detail||'首次使用时自动准备 FFmpeg';if(state.status==='downloading'&&number(state.totalBytes)>0)text+=' · '+formatBytes(state.downloadedBytes)+' / '+formatBytes(state.totalBytes);if(state.error)text+='：'+state.error;for(const id of ['ffmpegNotice','ffmpegSettingsStatus']){const target=$(id);empty(target);target.appendChild(element('span',state.status==='failed'?'error':'',text));if(state.status==='failed'&&app.viewer?.account?.admin)target.appendChild(button('重新检查 FFmpeg',retryFFmpeg));if(id==='ffmpegNotice')target.hidden=!['downloading','verifying','failed'].includes(state.status);}if(retryCovers)app.library.retryCovers();window.dramaPlayer?.updateDependency(state,text);}

async function pollTasks() {
  if (app.viewer?.onlineOnly) return;
  if (taskRequest) return pollPromise;
  taskRequest = true;
  pollPromise = (async () => {
    try {
      const result = await api('/api/ui/tasks');
      const signature = JSON.stringify([result.data || [], result.merges || {}]);
      if (signature !== taskSignature) {
        mergeStates = result.merges || {};
        renderTasks(result.data || []);
        taskSignature = signature;
      }
      const nextFFmpeg = JSON.stringify(result.ffmpeg || null);
      if (nextFFmpeg !== ffmpegSignature) {ffmpegSignature = nextFFmpeg; renderFFmpeg(result.ffmpeg);}
      $('taskError').textContent = '';
    } catch (error) {
      $('taskError').textContent = '任务刷新失败：' + error.message;
    } finally {taskRequest = false; schedule();}
  })();
  return pollPromise;
}

async function updateGroups(ids){if(!ids.length){setMessage('请先勾选下载合集',true);return;}try{await post('/api/ui/update',{ids});setMessage('已提交 '+ids.length+' 部更新检查，已有文件保留，只补充新增或缺失分集');await pollTasks();}catch(error){$('taskError').textContent='更新失败：'+error.message;}}

async function taskAction(action,ids,dramaIds=[]){const count=dramaIds.length||ids.length;const unit=dramaIds.length?' 部合集':' 个任务';if(!count){setMessage('所选任务中没有可执行此操作的项',true);return;}if(action==='cancel'&&!confirm('取消所选 '+count+unit+'？已完成文件保留，可稍后重试。'))return;try{const result=await post('/api/ui/tasks/'+action,dramaIds.length?{dramaIds}:{ids});$('taskError').textContent='';if(result.data)renderTasks(result.data);setMessage('已提交'+({pause:'暂停',resume:'继续',cancel:'取消',retry:'重试'}[action]||action)+'：'+count+unit);await pollTasks();}catch(error){$('taskError').textContent='操作失败：'+error.message;}}

async function clearTasks(){const ids=selectedTaskList().map(task=>task.id);if(!ids.length){setMessage('请先勾选需要清理的任务',true);return;}if(!confirm('清理勾选的 '+ids.length+' 个任务记录？进行中的任务会先停止；已下载视频不会删除。'))return;try{const result=await post('/api/ui/tasks/clear',{ids});ids.forEach(id=>selectedTasks.delete(id));renderTasks(result.data||[]);setMessage('已清理 '+(result.removed||0)+' 个任务'+(result.pending?'，另有 '+result.pending+' 个正在停止后清理':''));}catch(error){$('taskError').textContent='清理失败：'+error.message;}}

async function mergeGroups(ids){if(!ids.length){setMessage('请先勾选下载合集',true);return;}const deleteEpisodes=$('deleteEpisodesAfterMerge').checked;if(deleteEpisodes&&!confirm('合并成功后删除 '+ids.length+' 个合集已合并的分集文件，确认继续？'))return;setMessage('正在合并 '+ids.length+' 部，下载区会显示进度');try{const result=await post('/api/ui/merge',{dramaIds:ids,deleteEpisodes});const items=result.data||[];const failed=items.filter(item=>!item.ok);setMessage('合并完成：成功 '+(items.length-failed.length)+'，失败 '+failed.length,failed.length>0);await pollTasks();}catch(error){$('taskError').textContent='合并失败：'+error.message;}}


function renderActivity(all) {
  const ordered = all.slice().sort((left, right) => {
    const a = groupStats(left), b = groupStats(right);
    return Boolean(b.running + b.queued + b.parsing) - Boolean(a.running + a.queued + a.parsing);
  });
  const fragment = document.createDocumentFragment();
  for (const group of ordered.slice(0, 3)) {
    const stats = groupStats(group), row = element('div', 'activity-item');
    row.append(element('h3', '', group.title), element('p', 'small', stats.success + ' / ' + group.tasks.length + ' 集已完成'), progressBar(stats.percent));
    fragment.appendChild(row);
  }
  if (!ordered.length) fragment.appendChild(element('p', 'small', '暂无下载任务，可在剧集卡片上直接加入下载。'));
  $('activityTasks').replaceChildren(fragment);
}

async function enqueue(ids) {
  if (app.viewer?.onlineOnly) {setMessage('当前账号仅可在线观看', true); return false;}
  if (!ids.length) return false;
  try {
    const quality = downloadQuality;
    await post('/api/ui/download', {ids, quality});
    setMessage('已加入下载队列 · ' + ids.length + ' 部 · ' + qualityLabel(quality), false, {label: '查看下载', run: () => window.JukuDialogs.navigate('downloads')});
    await pollTasks();
    return true;
  } catch (error) {setMessage('加入下载失败：' + error.message, true); return false;}
}

function persistFilters() {
  savePreference('downloadFilters', {search: $('taskSearch').value, status: $('taskStatus').value, release: $('releaseStatus').value});
}

function toggleBatch() {
  batch = !batch;
  if (!batch) selectedTasks.clear();
  document.body.classList.toggle('download-batch', batch);
  $('downloadBatchControls').hidden = !batch;
  $('downloadBatchBtn').setAttribute('aria-pressed', String(batch));
  $('downloadBatchBtn').textContent = batch ? '退出批量' : '批量管理';
  renderTasks(tasks);
}

function schedule(restart = true) {
  if (app.viewer?.onlineOnly) return;
  const active = hasActiveTasks || ['idle', 'downloading', 'verifying'].includes(ffmpegStatus) || Object.values(mergeStates).some(state => state.status === 'running');
  const delay = document.hidden ? 30000 : active || !$('downloadsPage').hidden ? 2000 : 15000;
  if (!restart && polling && pollTimer && delay === scheduledDelay) return;
  clearTimeout(pollTimer);
  pollTimer = 0;
  if (!polling) return;
  scheduledDelay = delay;
  pollTimer = setTimeout(() => {pollTimer = 0; pollTasks();}, delay);
}

function init() {
  if (app.viewer?.onlineOnly) return;
  document.querySelectorAll('[data-download-quality]').forEach(setupQualitySelect);
  const stored = readPreference('downloadFilters', {}), saved = stored && typeof stored === 'object' ? stored : {};
  $('taskSearch').value = typeof saved.search === 'string' ? saved.search.slice(0, 80) : '';
  for (const [id, value] of [['taskStatus', saved.status], ['releaseStatus', saved.release]]) {
    $(id).value = Array.from($(id).options).some(option => option.value === value) ? value : '';
  }
  for (const id of ['taskStatus', 'releaseStatus']) $(id).addEventListener('change', () => {persistFilters(); renderTasks(tasks);});
  $('taskSearch').addEventListener('input', () => {persistFilters(); renderTasks(tasks);});
  $('downloadBatchBtn').addEventListener('click', toggleBatch);
  $('selectGroupsBtn').addEventListener('click', () => {visibleGroups.forEach(group => group.tasks.forEach(task => selectedTasks.add(task.id))); renderTasks(tasks);});
  $('invertGroupsBtn').addEventListener('click', () => {visibleGroups.forEach(group => {const checked = !group.tasks.every(task => selectedTasks.has(task.id)); group.tasks.forEach(task => checked ? selectedTasks.add(task.id) : selectedTasks.delete(task.id));}); renderTasks(tasks);});
  $('deselectGroupsBtn').addEventListener('click', () => {selectedTasks.clear(); renderTasks(tasks);});
  for (const [id, action, statuses] of [['pauseSelectedBtn','pause',['running','queued','parsing']],['resumeSelectedBtn','resume',['paused']],['cancelSelectedBtn','cancel',['running','queued','parsing','paused']],['retrySelectedBtn','retry',['failed','canceled']]]) $(id).addEventListener('click', () => taskAction(action, selectedTaskList().filter(task => statuses.includes(task.status)).map(task => task.id)));
  $('updateSelectedBtn').addEventListener('click', () => updateGroups(selectedGroupIDs()));
  $('mergeSelectedBtn').addEventListener('click', () => mergeGroups(selectedGroupIDs()));
  $('clearTasksBtn').addEventListener('click', clearTasks);
  window.addEventListener('downloadsChanged', pollTasks);
  document.addEventListener('visibilitychange', () => {if (!document.hidden) pollTasks(); schedule();});
  window.addEventListener('pagehide', () => {polling = false; clearTimeout(pollTimer);});
  window.addEventListener('pageshow', event => {polling = true; if (event.persisted) pollTasks(); else schedule();});
  polling = true;
  return pollTasks();
}

return {init, refresh: pollTasks, render: () => {renderTasks(tasks); schedule(false);}, enqueue, qualityControl, quality: () => downloadQuality, all: () => tasks};

}
