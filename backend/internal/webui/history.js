(() => {
  'use strict';
  const entries = new Map();
  let options, generation = 0, requestID = 0, busy = false, removing = false, limit = 500;
  const node = id => document.getElementById(id);
  const dialog = node('historyPanel');
  const list = node('historyList');
  const get = id => entries.get(id);

  function element(tag, className, text) {
    const result = document.createElement(tag);
    if (className) result.className = className;
    if (text !== undefined) result.textContent = text;
    return result;
  }

  function clock(value) {
    const seconds = Math.max(0, Math.floor(Number(value) || 0));
    const minutes = Math.floor(seconds / 60);
    return (minutes >= 60 ? Math.floor(minutes / 60) + ':' + String(minutes % 60).padStart(2, '0') : String(minutes).padStart(2, '0')) + ':' + String(seconds % 60).padStart(2, '0');
  }

  function progressText(id) {
    const entry = typeof id === 'object' ? id : get(id);
    if (!entry) return '';
    if (entry.completed) return entry.index >= entry.total ? '已看至最新' : '第' + entry.episode + '集已看完';
    return '看到第' + entry.episode + '集 ' + clock(entry.position);
  }

  function watchedTime(value) {
    const date = new Date(value);
    if (!Number.isFinite(date.getTime())) return '';
    const now = new Date(), elapsed = Math.max(0, now - date);
    if (elapsed < 60000) return '刚刚';
    if (elapsed < 3600000) return Math.floor(elapsed / 60000) + '分钟前';
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
    if (date >= yesterday) return (date >= today ? '今天 ' : '昨天 ') + date.toLocaleTimeString('zh-CN', {hour: '2-digit', minute: '2-digit', hour12: false});
    return date.toLocaleDateString('zh-CN', {year: date.getFullYear() === now.getFullYear() ? undefined : 'numeric', month: 'numeric', day: 'numeric'});
  }

  function controls() {
    node('clearHistoryBtn').disabled = busy || removing || !entries.size;
    node('historyContent').setAttribute('aria-busy', String(busy || removing));
    node('historySummary').textContent = '按最近观看排序 · 自动保存最近 ' + limit + ' 部';
  }

  function render() {
    controls();
    node('historyCount').textContent = entries.size ? entries.size + ' 部' : '';
    if (!dialog.open) return;
    const keyword = node('historySearch').value.trim().toLocaleLowerCase();
    const ordered = Array.from(entries.values()).sort((left, right) => new Date(right.watchedAt) - new Date(left.watchedAt));
    const visible = ordered.filter(entry => !keyword || (entry.title + ' ' + options.sourceLabel(entry.source)).toLocaleLowerCase().includes(keyword));
    const fragment = document.createDocumentFragment();
    for (const entry of visible) {
      const row = element('li', 'history-row');
      row.dataset.dramaId = entry.dramaId;
      const content = element('div', 'history-description');
      const title = element('div', 'history-title', entry.title || '短剧');
      title.title = entry.title;
      const meta = element('div', 'history-meta', [options.sourceLabel(entry.source), progressText(entry), watchedTime(entry.watchedAt)].filter(Boolean).join(' · '));
      meta.title = new Date(entry.watchedAt).toLocaleString();
      content.append(title, meta);
      const actions = element('div', 'history-actions');
      const resume = element('button', 'secondary', '继续观看');
      resume.setAttribute('aria-label', '继续观看 ' + entry.title);
      resume.addEventListener('click', () => {dialog.close(); options.play(entry.dramaId, entry.title);});
      const remove = element('button', 'secondary history-remove', '×');
      remove.title = '删除此记录';
      remove.setAttribute('aria-label', '删除 ' + entry.title + ' 的观看记录');
      remove.disabled = removing;
      remove.addEventListener('click', () => removeEntries(entry.dramaId));
      actions.append(resume, remove);
      row.append(content, actions);
      fragment.appendChild(row);
    }
    if (!visible.length) fragment.appendChild(element('li', 'history-empty', busy ? '正在读取观看记录…' : keyword ? '没有匹配的观看记录' : '还没有观看记录，播放后会自动保存'));
    list.replaceChildren(fragment);
  }

  function changed() {
    options?.onChanged();
    render();
  }

  async function refresh() {
    if (!options) return;
    const request = ++requestID, version = generation;
    busy = true;
    node('historyError').textContent = '';
    node('retryHistoryBtn').hidden = true;
    render();
    try {
      const result = await options.api('/api/ui/playback/history');
      if (request !== requestID || version !== generation) return;
      entries.clear();
      for (const entry of result.data || []) if (entry.dramaId) entries.set(entry.dramaId, entry);
      limit = result.limit || 500;
      changed();
    } catch (error) {
      if (request !== requestID) return;
      node('historyError').textContent = '读取观看记录失败：' + error.message;
      node('retryHistoryBtn').hidden = false;
    } finally {
      if (request === requestID) {busy = false; render();}
    }
  }

  function remember(entry) {
    if (!entry?.dramaId) return;
    entries.set(entry.dramaId, entry);
    generation++;
    if (entries.size > limit) {
      const oldest = Array.from(entries.values()).sort((left, right) => new Date(right.watchedAt) - new Date(left.watchedAt));
      for (const entry of oldest.slice(limit)) entries.delete(entry.dramaId);
    }
    changed();
  }

  async function save(session, progress, entry) {
    remember(entry);
    const version = generation;
    const result = await options.post('/api/ui/playback/progress', {session, progress});
    if (version === generation) {
      if (result.saved && result.entry?.dramaId) {
        entries.set(result.entry.dramaId, result.entry);
        changed();
      } else if (!result.saved) {
        refresh();
      }
    }
  }

  async function removeEntries(id) {
    if (removing || !id && !confirm('清空全部观看记录？已下载的视频和下载任务会保留。')) return;
    removing = true;
    node('historyError').textContent = '';
    render();
    try {
      await options.post('/api/ui/playback/history/remove', id ? {dramaId: id} : {all: true});
      generation++;
      if (id) entries.delete(id); else entries.clear();
      changed();
      node('historySearch').focus();
    } catch (error) {
      node('historyError').textContent = error.message;
    } finally {
      removing = false;
      render();
    }
  }

  function init(configuration) {
    options = configuration;
    node('openHistoryBtn').addEventListener('click', () => {
      node('historySearch').value = '';
      dialog.showModal();
      node('historyContent').scrollTop = 0;
      refresh();
    });
    node('closeHistoryBtn').addEventListener('click', () => dialog.close());
    node('clearHistoryBtn').addEventListener('click', () => removeEntries(''));
    node('retryHistoryBtn').addEventListener('click', refresh);
    node('historySearch').addEventListener('input', render);
    refresh();
  }

  function reportError(message) {
    node('historyError').textContent = '观看进度暂未保存：' + message;
    options?.onError(node('historyError').textContent);
  }

  window.JukuHistory = {init, get, list: () => Array.from(entries.values()), progressText, remember, save, refresh, reportError};
})();
