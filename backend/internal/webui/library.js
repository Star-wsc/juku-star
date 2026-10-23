import { createCardViewport } from './card-viewport.js';
import { createCoverRepair } from './cover-repair.js';
import { createDramaRefresh } from './drama-refresh.js';
import { createRecommendations } from './recommendations.js';
import { icon, initial, withFocus, readPreference, savePreference } from './ui-core.js';
import { $, element, empty, button, setMessage, api, post, valueText, firstNonEmpty, dramaTitle, watchLabel, historySuffix, normalizeSource, sourceKey, sourceLabel, categoryName, episodeCount, coverURL, tagsText, dramaSearchText, rebuildOptions, number, formatBytes, formatTime, statusText, releaseText, phaseText, progressText, progressBar, episodeLabel, groupStats } from './ui-core.js';

export function createLibrary(app) {
  const dramas = [], selected = new Set(), byID = new Map(), cards = $('cards');
  let visibleIDs = [], libraryRevision = 0, libraryRequest = false, libraryReload = false;
  let libraryUpdateRequest = false, libraryQueuedUpdate = false;
  let libraryTimer, libraryBusyText = '', libraryIsLoading = false, libraryMetadataRemaining = {};
  let sortMessage = '', onlineSearchMessage = '', onlineSearchQuery = '', onlineSearchIDs = new Set();
  let searchController, searchSequence = 0, batch = false, filterTimer;
  let pendingCategory = null, sourceWarning = '', readError = false;
  let vipMetadataRequest = false, vipMetadataMessage = '';
  const recommendations = createRecommendations({allowed: () => app.viewer?.sources?.includes('hongguo') !== false, merge: mergeRecommendations, changed: renderDramas, switched: () => {resetOnlineSearch(); toggleBatch(false); app.shell.resetScroll();}});
  const viewport = createCardViewport({container: cards, scroller: $('workspaceMain'), create: renderCard, key: dramaCardKey, rendered: refreshFollowing, update: (card, drama) => {
    card.classList.toggle('selected', selected.has(drama.id));
    card.querySelector('input[type=checkbox]').checked = selected.has(drama.id);
  }});
  const searchText = new WeakMap();
  const coverRepair = createCoverRepair({get: id => byID.get(id), post, apply: (id, address, previous) => {
    const drama = byID.get(id);
    if (!drama) return;
    drama.cover = address;
    drama.coverUrl = address;
    if (address === previous) {
      for (const card of cards.querySelectorAll('.card')) {if (card.dataset.dramaId === id) card.retryCover?.();}
    } else viewport.refresh();
    app.details.refreshCover(id, address === previous);
    app.following.refreshCover(id);
  }});
  const dramaRefresh = createDramaRefresh({post, apply: applyDramaRefresh, failure: (id) => {
    app.details.metadataStatus(id, '资料暂未更新，已保留原有信息；稍后重新打开可重试。');
    coverRepair.touch(id);
  }});
  function reconcileDrama(drama) {return coverRepair.reconcile(dramaRefresh.reconcile(drama));}
  function applyDramaRefresh(drama, warning) {
    const previous = byID.get(drama.id), index = dramas.findIndex(item => item.id === drama.id);
    if (index >= 0) dramas[index] = drama; else dramas.push(drama);
    byID.set(drama.id, drama);
    libraryRevision = 0;
    coverRepair.updated(drama.id);
    app.details.metadataStatus(drama.id, warning);
    rebuildChannels(false);
    renderDramas();
    app.following.render();
    app.details.refresh();
    if (previous && coverURL(previous) === coverURL(drama)) {
      for (const card of cards.querySelectorAll('.card')) {if (card.dataset.dramaId === drama.id) card.retryCover?.();}
      app.details.refreshCover(drama.id, true);
      app.following.refreshCover(drama.id);
    }
    window.dispatchEvent(new Event('jukulibrarychange'));
  }
  function refreshDrama(id) {
    if (!['hongguo', 'huangdou', 'huangguoai', 'huangguo-video'].includes(String(id).split(':')[0])) return;
    void dramaRefresh.refresh(id);
  }
  const renderTasks = () => app.downloads.render();
  const pollTasks = () => app.downloads.refresh();
function searchableText(drama) {if (!searchText.has(drama)) searchText.set(drama, dramaSearchText(drama)); return searchText.get(drama);}
function filteredDramas(){ const keyword=$('searchInput').value.trim().toLowerCase();const source=$('sourceSelect').value;const channel=$('channelSelect').value;return dramas.filter(dr=>{if(!window.JukuVIP.visible(dr))return false;if(source&&sourceKey(dr)!==source)return false;const cat=categoryName(dr);if(channel&&cat!==channel)return false;return !keyword||searchableText(dr).includes(keyword)||onlineSearchQuery===keyword&&onlineSearchIDs.has(dr.id);}); }

function rebuildSources(){ rebuildOptions($('sourceSelect'),app.viewer?.sources || ['huangguo','huangdou','hongguo'],'全部站源',sourceLabel,false); }

function rebuildChannels(reset){ const source=$('sourceSelect').value;const values=Array.from(new Set(dramas.filter(dr=>!source||sourceKey(dr)===source).map(categoryName))).sort((left,right)=>left.localeCompare(right,'zh-Hans-CN'));rebuildOptions($('channelSelect'),values,'全部分类',value=>value,reset);updateRefreshLabel(); }

function updateLibraryButton() {
  const source = recommendations.enabled ? 'hongguo' : $('sourceSelect').value, hongguo = !source || source === 'hongguo';
  const button = $('refreshBtn'), label = $('refreshBtnLabel');
  const busy = libraryUpdateRequest || libraryIsLoading;
  const text = busy ? '更新中…' : '更新剧库';
  if (label.textContent !== text) label.textContent = text;
  if (button.disabled !== busy) button.disabled = busy;
  if (button.getAttribute('aria-busy') !== String(busy)) button.setAttribute('aria-busy', String(busy));
  button.setAttribute('aria-label', (busy ? '正在更新' : '更新') + (source ? sourceLabel(source) : '全部站源') + '剧库');
  button.title = '检查新内容' + (hongguo ? '并继续加载历史目录' : '') + '，后台分批补充缺失资料；优先当前筛选结果' + (libraryMetadataRemaining[source] ? '，待检查 ' + libraryMetadataRemaining[source] + ' 部' : '');
}

function metadataPriorityIDs(){const pending=new Set(dramas.filter(drama=>!drama.sortMetadata||drama.sortMetadata.version!==(['huangguo','huangguoai','huangguoai.com'].includes(String(drama.source||drama.id).split(':')[0])?2:1)||sourceKey(drama)==='huangdou'&&drama.vip==null&&!drama.sortMetadata.vipChecked||sourceKey(drama)==='hongguo'&&!coverURL(drama)&&!drama.sortMetadata.coverChecked).map(drama=>drama.id));return visibleIDs.filter(id=>pending.has(id)).slice(0,40);}

function updateRefreshLabel(){ const source=$('sourceSelect').value;$('vipFilterBtn').hidden=app.viewer?.sources?.includes('huangdou')===false||Boolean(source&&source!=='huangdou');const hongguo=app.viewer?.sources?.includes('hongguo')!==false&&(!source||source==='hongguo');updateLibraryButton();$('onlineSearchBtn').hidden=!hongguo;$('searchInput').placeholder='搜索剧名、简介或标签';$('onlineSearchBtn').title='联网搜索红果，也可按回车'; }

function placeholder(text){ return element('div','cover placeholder',text||'暂无封面'); }

function dramaMetaText(drama) {
  const history = window.JukuHistory.progressText(drama.id);
  const count = episodeCount(drama);
  const status = drama.releaseStatus === 'finished' ? '已完结' : drama.releaseStatus === 'ongoing' ? '连载中' : '';
  return [history || (count ? count + ' 集' : '集数未知'), history ? '' : status].filter(Boolean).join(' · ');
}

function dramaCardKey(drama) {
  return JSON.stringify([dramaTitle(drama), sourceKey(drama), categoryName(drama), episodeCount(drama), coverURL(drama), drama.releaseStatus, drama.vip]);
}

function renderCard(drama) {
  const title = dramaTitle(drama);
  const card = element('article', 'card' + (selected.has(drama.id) ? ' selected' : ''));
  card.dataset.dramaId = drama.id;
  card.renderKey = dramaCardKey(drama);
  const label = element('label', 'card-select');
  label.dataset.downloadOnly = '';
  const checkbox = element('input');
  checkbox.type = 'checkbox';
  checkbox.checked = selected.has(drama.id);
  checkbox.dataset.focusKey = 'select-' + drama.id;
  checkbox.setAttribute('aria-label', '选择 ' + title);
  checkbox.addEventListener('change', () => {
    if (checkbox.checked) selected.add(drama.id); else selected.delete(drama.id);
    card.classList.toggle('selected', checkbox.checked);
    updateDramaSelection();
  });
  label.appendChild(checkbox);
  const hasProgress = Boolean(window.JukuHistory.get(drama.id));
  const poster = button('', () => app.play(drama.id, title), false, 'poster watch-button' + (hasProgress ? ' has-progress' : ''));
  poster.setAttribute('aria-label', (hasProgress ? '继续观看 ' : '播放 ') + title);
  poster.dataset.focusKey = 'play-' + drama.id;
  const cover = coverURL(drama);
  const fallback = element('span', 'cover placeholder');
  fallback.setAttribute('aria-hidden', 'true');
  fallback.append(element('span', '', initial(title)), element('small', '', cover ? '海报加载失败' : '暂无海报'));
  if (cover) {
    const image = element('img', 'cover');
    image.alt = '';
    image.loading = 'lazy';
    image.decoding = 'async';
    image.addEventListener('error', () => {image.replaceWith(fallback); coverRepair.failed(drama.id, cover);});
    image.addEventListener('load', () => coverRepair.loaded(drama.id, cover));
    image.src = cover;
    card.retryCover = () => {if (fallback.isConnected) {image.src = cover; fallback.replaceWith(image);}};
    poster.appendChild(image);
  } else poster.appendChild(fallback);
  poster.appendChild(element('span', 'poster-badge', sourceLabel(sourceKey(drama))));
  if (window.JukuVIP.isVIP(drama)) poster.appendChild(element('span', 'vip-badge', 'VIP'));
  const caption = element('span', 'poster-caption');
  caption.setAttribute('aria-hidden', 'true');
  caption.append(icon('play'), element('span', 'watch-label', hasProgress ? '续播' : '播放'), element('span', 'poster-category', categoryName(drama)));
  const progress = element('span', 'poster-progress');
  progress.setAttribute('aria-hidden', 'true');
  progress.appendChild(element('span'));
  progress.hidden = true;
  poster.append(caption, progress);
  const body = element('div', 'card-body');
  const name = button(title, () => app.details.open(drama.id), false, 'card-title');
  name.title = '查看详情 · ' + title;
  name.setAttribute('aria-label', '查看 ' + title + ' 详情');
  name.dataset.focusKey = 'title-' + drama.id;
  const meta = element('div', 'meta', dramaMetaText(drama));
  meta.title = dramaMetaText(drama);
  const footer = element('div', 'card-footer');
  const actions = element('div', 'card-actions');
  const download = button('', async () => {
    if (download.disabled) return;
    download.disabled = true;
    try {await app.downloads.enqueue([drama.id]);} finally {download.disabled = false;}
  }, false, 'quiet icon-button card-download');
  download.appendChild(icon('download'));
  download.title = '加入下载';
  download.setAttribute('aria-label', '下载 ' + title);
  download.dataset.focusKey = 'download-' + drama.id;
  actions.appendChild(app.following.saveButton(drama.id, title));
  if (!app.viewer?.onlineOnly) actions.appendChild(download);
  footer.append(meta, actions);
  body.append(name, footer);
  card.append(label, poster, body);
  return card;
}

function updateDramaSelection() {
  $('dramaSelection').textContent = '已选 ' + selected.size + ' 部';
  $('enqueueBtn').disabled = !selected.size;
}

function renderDramas() {
  clearTimeout(filterTimer);
  for (const id of selected) {if (!byID.has(id) || !window.JukuVIP.visible(byID.get(id))) selected.delete(id);}
  const filtered = filteredDramas();
  const mode = $('sortSelect').value;
  const list = recommendations.enabled ? recommendations.items().map(drama => byID.get(drama.id) || drama) : window.JukuLibrarySort.sortDramas(filtered, mode);
  sortMessage = recommendations.enabled ? '' : window.JukuLibrarySort.summary(filtered, mode);
  $('sortSelect').title = '缺少数据的剧集排在后面；不同站源的统计口径可能不同。' + sortMessage;
  visibleIDs = list.map(drama => drama.id);
  $('dramaCount').textContent = recommendations.enabled ? list.length + ' 部' : list.length === dramas.length ? list.length + ' 部' : list.length + ' / ' + dramas.length + ' 部';
  updateDramaSelection();
  updateLibraryStatus();
  refreshFilterSummary();
  let state = null;
  if (!list.length && recommendations.enabled) {
    state = element('div', 'empty', '红果分类推荐会显示在这里。');
  } else if (!list.length) {
    state = element('div', 'empty');
    state.append(element('strong', '', dramas.length ? '没有找到匹配的剧集' : '剧库还没有内容'), element('p', '', dramas.length ? '换个关键词，或清除当前筛选。' : '更新剧库即可获取可用站源的内容。'));
    state.appendChild(button(dramas.length ? '清除筛选' : '更新剧库', () => dramas.length ? resetFilters() : loadDramas(true), false, 'secondary'));
  }
  viewport.setItems(list, state);
}

async function loadDramas(update) {
  if (libraryRequest) {
    libraryReload = true;
    if (update) {libraryQueuedUpdate = true; libraryUpdateRequest = true; updateLibraryButton();}
    return;
  }
  libraryRequest = true;
  libraryUpdateRequest = Boolean(update);
  clearTimeout(libraryTimer);
  updateLibraryButton();
  const query = update ? '?update=1&source=' + encodeURIComponent(recommendations.enabled ? 'hongguo' : $('sourceSelect').value) + '&priority=' + encodeURIComponent(metadataPriorityIDs().join(',')) : '?revision=' + libraryRevision;
  try {
    const result = await api('/api/ui/dramas' + query);
    readError = false;
    libraryRevision = result.revision || 0;
    libraryIsLoading = Boolean(result.loading || result.metadata?.running);
    libraryMetadataRemaining = result.metadataRemaining || {};
    app.shell.sourceStates(result.sources);
    sourceWarning = result.error ? '部分站源尚未完成，可在“更多”查看状态' : '';
    if (Array.isArray(result.data)) {
      dramas.length = 0;
      byID.clear();
      for (let drama of result.data) {drama = reconcileDrama(drama); dramas.push(drama); byID.set(drama.id, drama);}
      for (const drama of recommendations.all()) {if (!byID.has(drama.id)) {dramas.push(drama); byID.set(drama.id, drama);}}
      rebuildSources();
      rebuildChannels(false);
      if (pendingCategory !== null) {
        const found = Array.from($('channelSelect').options).some(option => option.value === pendingCategory);
        if (found) $('channelSelect').value = pendingCategory;
        if (found || !result.loading) pendingCategory = null;
      }
      renderDramas();
      app.following.render();
      app.details.refresh();
      window.dispatchEvent(new Event('jukulibrarychange'));
    }
    const loaded = result.loadedAt && !result.loadedAt.startsWith('0001') ? new Date(result.loadedAt).toLocaleString() : '';
    $('libraryUpdatedAt').textContent = loaded ? '更新于 ' + loaded : '';
    $('libraryUpdatedAt').title = $('libraryUpdatedAt').textContent;
    $('libraryMenuUpdatedAt').textContent = $('libraryUpdatedAt').textContent;
    const metadata = result.metadata || {};
    if (!metadata.running && !vipMetadataRequest) vipMetadataMessage = '';
    libraryBusyText = result.loading ? '正在更新剧库，已有内容可继续使用' : metadata.running ? '后台补充资料 ' + metadata.checked + ' / ' + metadata.total : '';
    updateLibraryStatus();
    if (result.loading || metadata.running) libraryTimer = setTimeout(() => loadDramas(false), result.loading ? 1200 : 5000);
  } catch (error) {
    libraryIsLoading = false;
    readError = true;
    libraryBusyText = '剧库读取失败：' + error.message;
    if (!dramas.length) renderDramas();
    updateLibraryStatus();
  } finally {
    libraryRequest = false;
    if (libraryReload) {
      const queuedUpdate = libraryQueuedUpdate;
      libraryReload = false;
      libraryQueuedUpdate = false;
      loadDramas(queuedUpdate);
    } else {
      libraryUpdateRequest = false;
      updateLibraryButton();
    }
  }
}

function mergeRecommendations(items) {
  const positions = new Map(dramas.map((drama, index) => [drama.id, index]));
  for (let drama of items) {
    drama = reconcileDrama(drama);
    const index = positions.get(drama.id);
    if (index === undefined) {positions.set(drama.id, dramas.length); dramas.push(drama);} else dramas[index] = drama;
    byID.set(drama.id, drama);
  }
  libraryRevision = 0;
  rebuildSources();
  rebuildChannels(false);
  app.following.render();
  app.details.refresh();
}

function updateLibraryStatus() {
  const source = $('sourceSelect').value;
  const vip = recommendations.enabled ? '' : window.JukuVIP.summary(dramas.filter(drama => (!source || sourceKey(drama) === source)));
  const text = [libraryBusyText, onlineSearchMessage, sourceWarning, vip, vipMetadataMessage, sortMessage].filter(Boolean).join(' · ');
  $('libraryStatus').textContent = text;
  $('libraryFeedback').hidden = !text;
  $('retryLibraryBtn').hidden = !readError;
}

async function refreshVIPMetadata(priority = visibleIDs) {
  if (vipMetadataRequest || app.viewer?.sources?.includes('huangdou') === false) return;
  vipMetadataRequest = true;
  vipMetadataMessage = '正在检查 VIP 状态';
  updateLibraryStatus();
  try {
    const ids = priority.filter(id => id.startsWith('huangdou:')).slice(0, 40);
    const result = await post('/api/ui/vip/metadata', {priority: ids});
    vipMetadataMessage = result.pending ? '后台分批识别 VIP，也可点开剧集立即补充资料' : result.remaining ? '尚未识别的内容暂保留，可稍后点击“更新剧库”重试' : '';
    await loadDramas(false);
  } catch (_) {vipMetadataMessage = 'VIP 状态暂未补齐，可点击“更新剧库”重试';}
  finally {vipMetadataRequest = false; updateLibraryStatus();}
}

function resetOnlineSearch(){searchSequence++;if(searchController)searchController.abort();searchController=null;onlineSearchQuery='';onlineSearchIDs.clear();onlineSearchMessage='';$('onlineSearchBtn').disabled=false;$('onlineSearchBtn').textContent='联网搜索';updateLibraryStatus();}

async function searchOnline(){
    rememberSearch($('searchInput').value);
    if(app.viewer?.sources?.includes('hongguo')===false||$('sourceSelect').value&&$('sourceSelect').value!=='hongguo')return;
    const keyword=$('searchInput').value.trim();if(!keyword){setMessage('请先输入搜索词',true);return;}if(searchController)return;
    resetOnlineSearch();const sequence=searchSequence;const controller=new AbortController();searchController=controller;
    $('onlineSearchBtn').disabled=true;$('onlineSearchBtn').textContent='搜索中';onlineSearchMessage='正在联网搜索红果';updateLibraryStatus();
    try{
      const result=await api('/api/ui/search?q='+encodeURIComponent(keyword),{signal:controller.signal});
      if(sequence!==searchSequence||keyword!==$('searchInput').value.trim())return;
      const matches=(Array.isArray(result.data)?result.data:[]).filter(drama=>sourceKey(drama)==='hongguo');
      onlineSearchQuery=keyword.toLowerCase();onlineSearchIDs=new Set(matches.map(drama=>drama.id));

      const positions=new Map(dramas.map((drama,index)=>[drama.id,index]));for(let drama of matches){drama=reconcileDrama(drama);const position=positions.get(drama.id);if(position===undefined){positions.set(drama.id,dramas.length);dramas.push(drama);}else dramas[position]=drama;}
      onlineSearchMessage=matches.length?'联网返回 '+matches.length+' 部红果'+(result.total>matches.length?'（首批匹配）':''):'联网暂无匹配';
      if(matches.length&&result.saved===false)onlineSearchMessage+='，缓存未保存';
      for(const drama of dramas)byID.set(drama.id,drama);rebuildSources();rebuildChannels(false);renderDramas();app.following.render();updateLibraryStatus();libraryRevision=0;await loadDramas(false);
    }catch(error){if(sequence===searchSequence&&!controller.signal.aborted){onlineSearchMessage='联网搜索暂不可用：'+error.message+'；可重试，本地筛选仍可使用';updateLibraryStatus();}}
    finally{if(sequence===searchSequence){searchController=null;$('onlineSearchBtn').disabled=false;$('onlineSearchBtn').textContent='联网搜索';}}
  }

async function enqueueSelected() {
  if (!selected.size) {setMessage('请先选择剧集', true); return;}
  $('enqueueBtn').disabled = true;
  const ok = await app.downloads.enqueue(Array.from(selected));
  if (ok) toggleBatch(false);
  updateDramaSelection();
}



function refreshFilterSummary() {
  for (const id of ['sourceSelect', 'channelSelect']) $(id).title = $(id).selectedOptions[0]?.textContent || '';
  $('resetFiltersBtn').hidden = recommendations.enabled || !$('searchInput').value && !$('channelSelect').value && $('sortSelect').value === 'default' && ['', 'hongguo'].includes($('sourceSelect').value);
  $('clearSearchBtn').hidden = !$('searchInput').value;
}

function persistFilters() {
  savePreference('libraryFilters', {source: $('sourceSelect').value, category: $('channelSelect').value, sort: $('sortSelect').value, search: $('searchInput').value});
}

function resetFilters() {
  resetOnlineSearch();
  $('sourceSelect').value = '';
  $('searchInput').value = '';
  $('sortSelect').value = 'default';
  pendingCategory = null;
  rebuildChannels(true);
  renderDramas();
  persistFilters();
  app.shell.resetScroll();
}

function toggleBatch(value) {
  batch = value;
  if (!batch) selected.clear();
  document.body.classList.toggle('library-batch', batch);
  $('libraryBatchBar').hidden = !batch;
  $('batchSelectBtn').setAttribute('aria-pressed', String(batch));
  $('batchSelectLabel').textContent = batch ? '退出批量' : '批量选择';
  $('batchSelectBtn').setAttribute('aria-label', batch ? '退出批量选择' : '批量选择');
  $('batchSelectBtn').title = batch ? '退出批量选择' : '批量选择';
  renderDramas();
}

function refreshFollowing(nodes = cards.querySelectorAll('.card')) {
  for (const card of nodes) {
    const id = card.dataset.dramaId, drama = byID.get(id);
    if (!drama) continue;
    const save = card.querySelector('[data-save-id]');
    if (save) {
      const saved = Boolean(app.following.get(id)?.saved);
      save.setAttribute('aria-pressed', String(saved));
      save.setAttribute('aria-label', (saved ? '取消收藏 ' : '收藏 ') + dramaTitle(drama));
      save.title = saved ? '取消收藏' : '加入想看';
      save.disabled = app.following.busy(id);
    }
    const history = window.JukuHistory.get(id);
    const watch = card.querySelector('.watch-button');
    watch.querySelector('.watch-label').textContent = history ? '续播' : '播放';
    watch.classList.toggle('has-progress', Boolean(history));
    watch.setAttribute('aria-label', (history ? '继续观看 ' : '播放 ') + dramaTitle(drama));
    const progress = watch.querySelector('.poster-progress');
    progress.hidden = !(history?.duration > 0);
    progress.firstElementChild.style.width = history?.duration > 0 ? Math.min(100, Math.max(0, history.position / history.duration * 100)) + '%' : '0%';
    card.querySelector('.meta').textContent = dramaMetaText(drama);
    card.querySelector('.meta').title = dramaMetaText(drama);
  }
}

function searchHistory() {
  const items = readPreference('recentSearches', []);
  return Array.isArray(items) ? items.filter(item => typeof item === 'string' && item.length <= 80).slice(0, 10) : [];
}

function rememberSearch(raw) {
  const query = raw.trim().slice(0, 80);
  if (query) savePreference('recentSearches', [query, ...searchHistory().filter(item => item !== query)].slice(0, 10));
  $('recentSearches').hidden = true;
}

function renderSearchHistory() {
  const list = searchHistory(), target = $('recentSearches');
  target.replaceChildren();
  target.hidden = !list.length;
  if (!list.length) return;
  target.appendChild(element('span', 'small', '最近搜索'));
  list.forEach(query => target.appendChild(button(query, () => {
    $('searchInput').value = query;
    resetOnlineSearch();
    renderDramas();
    persistFilters();
    target.hidden = true;
  }, false, 'secondary')));
  target.appendChild(button('清除记录', () => {savePreference('recentSearches', []); target.hidden = true;}, false, 'quiet'));
}

function init() {
  recommendations.init();
  window.addEventListener('jukuvipfilterchange', event => {renderDramas(); app.shell.resetScroll(); if (event.detail?.interactive) void refreshVIPMetadata(event.detail.priority || visibleIDs);});
  rebuildSources();
  const stored = readPreference('libraryFilters', {});
  const saved = stored && typeof stored === 'object' ? stored : {};
  const allowedSources = app.viewer?.sources || ['huangguo','huangdou','hongguo'];
  $('sourceSelect').value = ['', ...allowedSources].includes(saved.source) ? saved.source : allowedSources.includes('hongguo') ? 'hongguo' : allowedSources[0] || '';
  $('sortSelect').value = window.JukuLibrarySort.modes.includes(saved.sort) ? saved.sort : 'default';
  $('searchInput').value = typeof saved.search === 'string' ? saved.search.slice(0, 80) : '';
  pendingCategory = typeof saved.category === 'string' ? saved.category : null;
  rebuildChannels(false);
  refreshFilterSummary();
  const filters = () => {persistFilters(); renderDramas(); app.shell.resetScroll();};
  $('refreshBtn').addEventListener('click', () => loadDramas(true));
  $('retryLibraryBtn').addEventListener('click', () => loadDramas(false));
  $('searchInput').addEventListener('input', () => {
    resetOnlineSearch();
    persistFilters();
    refreshFilterSummary();
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {renderDramas(); app.shell.resetScroll();}, 90);
  });
  $('searchInput').addEventListener('focus', renderSearchHistory);
  $('searchInput').addEventListener('blur', () => setTimeout(() => {
    if (!$('recentSearches').contains(document.activeElement)) $('recentSearches').hidden = true;
  }, 150));
  $('searchInput').addEventListener('keydown', event => {
    if (event.key === 'Enter' && !event.isComposing) {event.preventDefault(); searchOnline();}
  });
  $('clearSearchBtn').addEventListener('click', () => {$('searchInput').value = ''; resetOnlineSearch(); filters(); $('searchInput').focus();});
  $('onlineSearchBtn').addEventListener('click', searchOnline);
  $('sourceSelect').addEventListener('change', () => {pendingCategory = null; resetOnlineSearch(); rebuildChannels(true); filters();});
  $('channelSelect').addEventListener('change', filters);
  $('sortSelect').addEventListener('change', filters);
  $('resetFiltersBtn').addEventListener('click', resetFilters);
  $('batchSelectBtn').addEventListener('click', () => toggleBatch(!batch));
  $('finishBatchBtn').addEventListener('click', () => toggleBatch(false));
  $('selectVisibleBtn').addEventListener('click', () => {visibleIDs.forEach(id => selected.add(id)); renderDramas();});
  $('invertVisibleBtn').addEventListener('click', () => {visibleIDs.forEach(id => selected.has(id) ? selected.delete(id) : selected.add(id)); renderDramas();});
  $('deselectDramasBtn').addEventListener('click', () => {selected.clear(); renderDramas();});
  $('enqueueBtn').addEventListener('click', enqueueSelected);
  for (let index = 0; index < 12; index++) {const item = element('div', 'skeleton-card'); item.setAttribute('aria-hidden', 'true'); cards.appendChild(item);}
  return loadDramas(false);
}

return {init, refreshDrama, layout: viewport.refresh, get: id => byID.get(id), all: () => dramas, render: renderDramas, refreshFollowing, repairCover: coverRepair.touch, coverFailed: coverRepair.failed, coverLoaded: coverRepair.loaded, refresh: () => {libraryRevision = 0; return loadDramas(false);}, retryCovers: () => {cards.querySelectorAll('.card').forEach(card => card.retryCover?.()); app.details.retryCover(); app.following.retryCovers();}};

}
