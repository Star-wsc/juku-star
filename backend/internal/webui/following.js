import { $, element, button, icon, initial, withFocus, dramaTitle, sourceKey, sourceLabel, episodeCount, coverURL, number, setMessage } from './ui-core.js';

export function createFollowing(app) {
  const entries = new Map(), pending = new Set();
  let tab = 'watching', loaded = false, loading = false, generation = 0, requestID = 0, visibleLimit = 100;
  let lastRefresh = 0, refreshPromise;

  function get(id) {
    const entry = entries.get(id);
    if (!entry) return undefined;
    const drama = app.library?.get(id);
    const total = number(drama && episodeCount(drama)) || entry.totalEpisode || entry.knownEpisodes || 0;
    return {...entry, title: drama ? dramaTitle(drama) : entry.title, totalEpisode: total, newEpisodes: entry.knownEpisodes > 0 ? Math.max(0, total - entry.knownEpisodes) : 0};
  }

  function records() {
    const history = new Map((window.JukuHistory.list?.() || []).map(entry => [entry.dramaId, entry]));
    return Array.from(new Set([...history.keys(), ...entries.keys()])).map(id => {
      const entry = get(id), watched = history.get(id), drama = app.library?.get(id);
      const total = number(drama && episodeCount(drama)) || entry?.totalEpisode || watched?.total || 0;
      const completed = Boolean(entry?.completed && !entry.newEpisodes || watched?.completed && watched.index >= Math.max(total, watched.total));
      return {id, cover: drama ? coverURL(drama) : '', title: drama ? dramaTitle(drama) : entry?.title || watched?.title || '短剧', source: drama ? sourceKey(drama) : entry?.source || watched?.source, history: watched, entry, total, completed, watching: !completed && Boolean(watched || entry?.completed), saved: Boolean(entry?.saved), newEpisodes: entry?.newEpisodes || 0};
    }).sort((left, right) => new Date(right.history?.watchedAt || right.entry?.updatedAt || 0) - new Date(left.history?.watchedAt || left.entry?.updatedAt || 0));
  }

  function renderContinue(all) {
    const list = all.filter(entry => entry.watching).slice(0, 6);
    $('continueSection').hidden = !list.length;
    withFocus($('continueList'), () => {
      $('continueList').replaceChildren();
      for (const record of list) {
        const control = button('', () => app.play(record.id, record.title), false, 'continue-card');
        control.dataset.focusKey = 'continue-' + record.id;
        control.setAttribute('aria-label', '继续观看 ' + record.title);
        control.title = record.title + (record.history ? ' · ' + window.JukuHistory.progressText(record.history) : ' · 有新集');
        const identity = element('span', 'continue-initial', initial(record.title));
        identity.setAttribute('aria-hidden', 'true');
        const content = element('span', 'continue-content');
        content.append(element('strong', '', record.title), element('span', 'small', record.history?.index > 0 ? '第 ' + record.history.index + ' 集' : '接着看'));
        const progress = element('span', 'progress');
        const fill = element('span');
        const history = record.history;
        fill.style.width = history?.duration > 0 ? Math.min(100, Math.max(0, history.position / history.duration * 100)) + '%' : '0%';
        progress.appendChild(fill);
        content.appendChild(progress);
        const play = icon('play');
        play.classList.add('continue-play');
        control.append(identity, content, play);
        $('continueList').appendChild(control);
      }
    });
  }

  function renderCover(record) {
    const poster = element('span', 'following-cover');
    poster.setAttribute('aria-hidden', 'true');
    const fallback = element('span', '', initial(record.title));
    if (record.cover) {
      const image = element('img');
      image.alt = '';
      image.loading = 'lazy';
      image.decoding = 'async';
      image.addEventListener('error', () => {image.replaceWith(fallback); app.library?.coverFailed(record.id, record.cover);});
      image.addEventListener('load', () => app.library?.coverLoaded(record.id, record.cover));
      image.src = record.cover;
      poster.retryCover = () => {if (fallback.isConnected) {image.src = record.cover; fallback.replaceWith(image);}};
      poster.appendChild(image);
    } else poster.appendChild(fallback);
    return poster;
  }

  function refreshCover(id) {
    const drama = app.library?.get(id);
    if (!drama) return;
    for (const row of $('followingList').querySelectorAll('.following-row')) {
      if (row.dataset.dramaId === id) row.querySelector('.following-cover')?.replaceWith(renderCover({id, title: dramaTitle(drama), cover: coverURL(drama)}));
    }
  }

  function render() {
    const all = records();
    const lists = {watching: all.filter(entry => entry.watching), saved: all.filter(entry => entry.saved && !entry.watching && !entry.completed), completed: all.filter(entry => entry.completed)};
    $('watchingCount').textContent = lists.watching.length;
    $('savedCount').textContent = loaded ? lists.saved.length : '—';
    $('completedCount').textContent = lists.completed.length;
    document.querySelectorAll('[data-follow-count]').forEach(node => {node.textContent = lists.watching.length; node.hidden = !lists.watching.length;});
    document.querySelectorAll('[data-follow-tab]').forEach(node => node.setAttribute('aria-pressed', String(node.dataset.followTab === tab)));
    renderContinue(all);
    if ($('followingPage').hidden) return;
    const keyword = $('followingSearch').value.trim().toLocaleLowerCase();
    const list = lists[tab].filter(entry => (entry.title + ' ' + sourceLabel(entry.source)).toLocaleLowerCase().includes(keyword));
    withFocus($('followingList'), () => {
      const fragment = document.createDocumentFragment();
      for (const record of list.slice(0, visibleLimit)) {
        const row = element('article', 'following-row');
        row.dataset.dramaId = record.id;
        const identity = renderCover(record);
        const description = element('div', 'following-description');
        const name = button(record.title, () => app.details.open(record.id), false, 'following-title');
        name.dataset.focusKey = 'following-title-' + record.id;
        const progress = record.entry?.completed ? '手动标为已看' : record.history ? window.JukuHistory.progressText(record.history) : '尚未开始';
        description.append(name, element('p', 'small', [sourceLabel(record.source), progress, record.total ? record.total + ' 集' : '集数待补充'].join(' · ')));
        if (record.newEpisodes) description.appendChild(element('p', 'following-update', '剧库新增 ' + record.newEpisodes + ' 集'));
        const actions = element('div', 'following-actions');
        const play = button(record.completed ? '重看' : record.watching ? '继续观看' : '开始看', () => app.play(record.id, record.title), false, record.watching ? '' : 'subtle');
        play.dataset.focusKey = 'following-play-' + record.id;
        const save = saveButton(record.id, record.title);
        actions.append(play, save);
        row.append(identity, description, actions);
        fragment.appendChild(row);
      }
      if (list.length > visibleLimit) fragment.appendChild(button('显示更多 · 还有 ' + (list.length - visibleLimit) + ' 部', () => {visibleLimit += 100; render();}, false, 'secondary'));
      if (!list.length) {
        const empty = element('div', 'empty');
        const title = loading ? '正在读取追剧清单…' : !loaded && tab === 'saved' ? '追剧清单暂未读取' : keyword ? '没有匹配的剧集' : {watching: '还没有正在观看的剧集', saved: '先收藏一部想看的剧', completed: '还没有已看完的剧集'}[tab];
        empty.append(element('strong', '', title), element('p', '', keyword ? '试试其他关键词。' : tab === 'saved' ? '在剧集卡片或详情中点击收藏，就能在这里找到。' : '观看进度会自动保存，也可以在详情里手动标记已看。'));
        empty.appendChild(button('去剧库看看', () => window.JukuDialogs.navigate('library'), false, 'secondary'));
        fragment.appendChild(empty);
      }
      $('followingList').replaceChildren(fragment);
    });
  }

  function changed() {
    render();
    app.library?.refreshFollowing();
    app.details?.refresh();
  }

  async function refresh() {
    if (loading) return refreshPromise;
    const request = ++requestID, version = generation;
    loading = true;
    $('followingError').textContent = '';
    $('retryFollowingBtn').hidden = true;
    render();
    refreshPromise = (async () => {
      try {
        const result = await app.api('/api/ui/following');
        if (request !== requestID || generation !== version) return;
        entries.clear();
        for (const entry of result.data || []) if (entry.dramaId) entries.set(entry.dramaId, entry);
        loaded = true;
        lastRefresh = Date.now();
        changed();
      } catch (error) {
        $('followingError').textContent = '读取追剧清单失败：' + error.message;
        $('retryFollowingBtn').hidden = false;
      } finally {
        loading = false;
        render();
      }
    })();
    return refreshPromise;
  }

  async function change(id, values, message, undo) {
    if (pending.has(id)) return false;
    if (!loaded) {await refresh(); if (!loaded) {setMessage('请先重试读取追剧清单', true); return false;}}
    pending.add(id);
    generation++;
    changed();
    try {
      const result = await app.post('/api/ui/following', {dramaId: id, ...values});
      generation++;
      if (result.entry?.saved || result.entry?.completed) entries.set(id, result.entry);
      else entries.delete(id);
      $('followingError').textContent = '';
      if (message) setMessage(message, false, undo ? {label: '撤销', run: () => change(id, undo, '已撤销')} : null);
      return true;
    } catch (error) {
      $('followingError').textContent = error.message;
      setMessage(error.message, true);
      return false;
    } finally {
      pending.delete(id);
      changed();
    }
  }

  async function toggleSaved(id) {
    if (!loaded) {await refresh(); if (!loaded) {setMessage('追剧清单尚未读取，请重试', true); return;}}
    const value = !entries.get(id)?.saved;
    return change(id, {saved: value}, value ? '已加入追剧清单' : '已取消收藏', {saved: !value});
  }

  function setCompleted(id, value) {
    return change(id, {completed: value}, value ? '已标为已看，播放进度仍保留' : '已取消手动已看标记', {completed: !value});
  }

  function saveButton(id, title) {
    const saved = get(id)?.saved;
    const control = button('', () => toggleSaved(id), pending.has(id), 'secondary icon-button save-button');
    control.appendChild(icon('bookmark'));
    control.dataset.focusKey = 'save-' + id;
    control.dataset.saveId = id;
    control.setAttribute('aria-pressed', String(Boolean(saved)));
    control.setAttribute('aria-label', (saved ? '取消收藏 ' : '收藏 ') + title);
    control.title = saved ? '取消收藏' : '加入想看';
    return control;
  }

  function showTab(value) {
    if (!['watching', 'saved', 'completed'].includes(value)) return;
    tab = value;
    visibleLimit = 100;
    render();
  }

  function init() {
    document.querySelectorAll('[data-follow-tab]').forEach(control => control.addEventListener('click', () => showTab(control.dataset.followTab)));
    $('followingSearch').addEventListener('input', () => {visibleLimit = 100; render();});
    $('retryFollowingBtn').addEventListener('click', refresh);
    document.addEventListener('visibilitychange', () => {if (!document.hidden && Date.now() - lastRefresh > 30000) refresh();});
    return refresh();
  }

  return {init, render, refresh, refreshCover, retryCovers: () => $('followingList').querySelectorAll('.following-cover').forEach(poster => poster.retryCover?.()), get, toggleSaved, setCompleted, saveButton, showTab, busy: id => pending.has(id), acknowledge: id => {if (get(id)?.newEpisodes) change(id, {acknowledge: true});}};
}
