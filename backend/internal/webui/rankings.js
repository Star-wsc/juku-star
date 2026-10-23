(() => {
  'use strict';
  window.JukuRankings = {init};

  function init({api, post, getSource, getDrama = () => null, sourceLabel, onLibraryChanged, onDownloadsChanged, getDownloadQuality = () => 0, canDownload = () => true, play}) {
    const $ = id => document.getElementById(id);
    const dialog = $('rankingPanel');
    const list = $('rankingList');
    const tabs = $('rankingTabs');
    let boards = [], board = null, entries = [], page = 0, hasMore = false;
    let busy = false, controller = null, sequence = 0, changed = false, fetchedAt = '', updatedText = '';
    const lastBoards = new Map(), submitted = new Set(), submitting = new Set();

    function node(tag, className, text) {
      const result = document.createElement(tag);
      if (className) result.className = className;
      if (text !== undefined) result.textContent = text;
      return result;
    }
    function action(text, handler) {
      const result = node('button', 'secondary', text);
      result.type = 'button';
      result.addEventListener('click', handler);
      return result;
    }
    function cancel() {
      sequence++;
      if (controller) controller.abort();
      controller = null;
      busy = false;
    }
    function controls() {
      $('refreshRankingBtn').disabled = busy;
      $('rankingMoreBtn').disabled = busy;
      $('rankingMoreBtn').hidden = !hasMore;
      $('rankingMoreBtn').textContent = busy ? '加载中…' : '加载更多';
      $('rankingContent').setAttribute('aria-busy', String(busy));
    }
    function state(text) {
      list.replaceChildren(node('li', 'ranking-state', text));
    }
    function setError(text) { $('rankingError').textContent = text || ''; }
    function renderEntries() {
      const visible = entries.filter(item => window.JukuVIP.visible(dramaFor(item)));
      list.replaceChildren(...visible.map(renderEntry));
      if (!visible.length) state(entries.length ? '本页 VIP 内容已隐藏，可将 VIP 开关切换为“显示”或继续加载。' : '此榜单暂时没有条目');
    }
    window.addEventListener('jukuvipfilterchange', event => {if (dialog.open) {renderEntries(); updateSummary(false); if (event.detail?.interactive) event.detail.priority = entries.map(item => item.drama.id);}});
    function updateSummary(stale) {
      $('rankingSummary').textContent = (updatedText || board?.description || '按站点名次展示') + (entries.length ? ' · 显示 ' + entries.filter(item => window.JukuVIP.visible(dramaFor(item))).length + ' / ' + entries.length + ' 部' : '') + (board?.source === 'huangdou' ? ' · ' + window.JukuVIP.summary(entries.map(dramaFor)) : '');
      $('rankingFetchedAt').textContent = fetchedAt ? (stale ? '上次获取：' : '获取于 ') + new Date(fetchedAt).toLocaleString() : '';
    }

    function dramaFor(item) {
      const known = getDrama(item.drama.id) || {};
      if ((Date.parse(known.sortMetadata?.checkedAt) || 0) > (Date.parse(fetchedAt) || 0)) return {...item.drama, ...known};
      const drama = {...known, ...item.drama};
      for (const [key, value] of Object.entries(known)) {if (drama[key] == null || drama[key] === '') drama[key] = value;}
      return drama;
    }
    window.addEventListener('jukulibrarychange', () => {if (dialog.open) {renderEntries(); updateSummary(false);}});

    function renderEntry(item) {
      const drama = dramaFor(item), title = drama.title || drama.name || '短剧';
      const row = node('li', 'ranking-row');
      row.value = item.rank;
      const rank = node('span', 'ranking-number' + (item.rank <= 3 ? ' top' : ''), String(item.rank).padStart(2, '0'));
      rank.setAttribute('aria-label', '第 ' + item.rank + ' 名');
      const content = node('div');
      const heading = node('div', 'ranking-title', title);
      heading.title = title;
      content.appendChild(heading);
      const count = drama.totalEpisode || drama.episodeCount;
      const details = [window.JukuVIP.isVIP(drama) ? 'VIP · 仅试看' : '', item.metric, drama.remark || (count ? count + ' 集' : ''), drama.categoryName].filter(Boolean);
      if (details.length) content.appendChild(node('div', 'ranking-meta', details.join(' · ')));
      const actions = node('div', 'ranking-actions');
      const watchLabel = window.JukuHistory?.get(drama.id) ? '继续观看' : '播放';
      const watch = action(watchLabel, () => { dialog.close(); play(drama.id, title); });
      watch.setAttribute('aria-label', watchLabel + ' ' + title);
      const download = action(submitted.has(drama.id) ? '已提交' : '下载', async () => {
        if (!canDownload() || submitting.has(drama.id) || submitted.has(drama.id)) return;
        submitting.add(drama.id);
        download.disabled = true;
        download.textContent = '提交中';
        try {
          await post('/api/ui/download', {ids: [drama.id], quality: getDownloadQuality()});
          submitted.add(drama.id);
          download.textContent = '已提交';
          $('rankingActionStatus').textContent = '已将《' + title + '》加入下载队列';
          onDownloadsChanged();
        } catch (error) {
          download.textContent = '下载';
          download.disabled = false;
          $('rankingActionStatus').textContent = '加入下载失败：' + error.message;
        } finally {
          submitting.delete(drama.id);
        }
      });
      download.disabled = submitted.has(drama.id) || submitting.has(drama.id);
      download.setAttribute('aria-label', '下载 ' + title);
      actions.appendChild(watch);
      if (canDownload()) actions.appendChild(download);
      row.append(rank, content, actions);
      return row;
    }

    async function loadPage(nextPage, refresh = false) {
      if (!board) return;
      cancel();
      const ticket = sequence, activeBoard = board;
      controller = new AbortController();
      const signal = controller.signal;
      busy = true;
      setError('');
      $('rankingActionStatus').textContent = '';
      if (!entries.length) state('正在获取站点榜单…');
      controls();
      try {
        const result = await api('/api/ui/rankings?board=' + encodeURIComponent(activeBoard.id) + '&page=' + nextPage + (refresh ? '&refresh=1' : ''), {signal});
        if (ticket !== sequence || !dialog.open) return;
        if (result.boardId !== activeBoard.id || result.page !== nextPage || !Array.isArray(result.items)) throw new Error('榜单响应不完整，请刷新重试');
        if (nextPage > 1 && result.stale) throw new Error('此页暂时只能取得旧榜单，请刷新后继续');
        const incoming = result.items;
        let previousRank = nextPage > 1 && entries.length ? entries[entries.length - 1].rank : 0;
        const ids = new Set(nextPage > 1 ? entries.map(item => item.drama.id) : []);
        for (const item of incoming) {
          if (!item.drama?.id || !Number.isInteger(item.rank) || item.rank <= previousRank || ids.has(item.drama.id)) throw new Error('榜单顺序已变化，请刷新后继续');
          previousRank = item.rank;
          ids.add(item.drama.id);
        }
        if (nextPage > 1 && updatedText && result.updatedText && result.updatedText !== updatedText) throw new Error('站点榜单已更新，请刷新后继续');
        if (nextPage === 1) {
          entries = [];
          list.replaceChildren();
          $('rankingContent').scrollTop = 0;
          fetchedAt = result.fetchedAt || '';
          updatedText = result.updatedText || '';
        }
        entries.push(...incoming);
        renderEntries();
        page = nextPage;
        hasMore = Boolean(result.hasMore) && incoming.length > 0 && !result.stale;
        changed = changed || incoming.length > 0;
        if (!entries.length) state('此榜单暂时没有条目');
        updateSummary(result.stale);
        setError([result.warning, result.saved === false && incoming.length ? '剧库缓存未保存，本次仍可播放和下载。' : ''].filter(Boolean).join(' '));
      } catch (error) {
        if (ticket !== sequence || signal.aborted) return;
        setError(error.message + (entries.length ? '；已保留当前列表。' : ''));
        if (!entries.length) state('暂时无法取得榜单，点击“刷新”重试');
      } finally {
        if (ticket === sequence) { busy = false; controller = null; controls(); }
      }
    }

    function selectBoard(id) {
      board = boards.find(item => item.id === id);
      if (!board) return;
      $('rankingVipFilterBtn').hidden = board.source !== 'huangdou';
      lastBoards.set(board.source, board.id);
      entries = []; page = 0; hasMore = false; fetchedAt = ''; updatedText = '';
      for (const tab of tabs.children) {
        const active = tab.dataset.board === id;
        tab.setAttribute('aria-selected', String(active));
        tab.tabIndex = active ? 0 : -1;
      }
      $('rankingContent').setAttribute('aria-labelledby', 'ranking-tab-' + id);
      updateSummary(false);
      loadPage(1);
    }

    function selectSource() {
      const source = $('rankingSource').value;
      const available = boards.filter(item => item.source === source);
      tabs.replaceChildren();
      for (const item of available) {
        const tab = action(item.name, () => selectBoard(item.id));
        tab.id = 'ranking-tab-' + item.id;
        tab.dataset.board = item.id;
        tab.setAttribute('role', 'tab');
        tab.setAttribute('aria-controls', 'rankingContent');
        tabs.appendChild(tab);
      }
      selectBoard(available.find(item => item.id === lastBoards.get(source))?.id || available[0]?.id);
    }

    async function loadCatalog() {
      cancel();
      const ticket = sequence;
      busy = true;
      controller = new AbortController();
      const signal = controller.signal;
      controls();
      setError('');
      state('正在读取可用榜单…');
      try {
        if (!boards.length) {
          const result = await api('/api/ui/rankings', {signal});
          if (ticket !== sequence || !dialog.open) return;
          boards = Array.isArray(result.boards) ? result.boards : [];
        }
        if (!boards.length) throw new Error('暂未提供可用榜单');
        const sources = [...new Set(boards.map(item => item.source))];
        const wanted = getSource() || $('rankingSource').value;
        $('rankingSource').replaceChildren(...sources.map(source => {
          const option = node('option', '', sourceLabel(source));
          option.value = source;
          return option;
        }));
        $('rankingSource').value = sources.includes(wanted) ? wanted : sources[0];
        selectSource();
      } catch (error) {
        if (ticket !== sequence || signal.aborted) return;
        state('点击“刷新”重试');
        setError(error.message);
      } finally {
        if (ticket === sequence) { busy = false; controller = null; controls(); }
      }
    }

    $('openRankingsBtn').addEventListener('click', () => {
      dialog.showModal();
      $('closeRankingsBtn').focus();
      $('rankingActionStatus').textContent = '';
      loadCatalog();
    });
    $('closeRankingsBtn').addEventListener('click', () => dialog.close());
    $('refreshRankingBtn').addEventListener('click', () => board ? loadPage(1, true) : loadCatalog());
    $('rankingMoreBtn').addEventListener('click', () => { if (!busy && hasMore) loadPage(page + 1); });
    $('rankingSource').addEventListener('change', selectSource);
    tabs.addEventListener('keydown', event => {
      const buttons = [...tabs.children], index = buttons.indexOf(event.target);
      if (index < 0 || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      event.preventDefault();
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
      buttons[next].focus();
      selectBoard(buttons[next].dataset.board);
    });
    dialog.addEventListener('click', event => {
      if (event.target !== dialog) return;
      const box = dialog.getBoundingClientRect();
      if (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom) dialog.close();
    });
    dialog.addEventListener('close', () => {

      if (dialog.open) return;
      cancel();
      controls();
      if (changed) { changed = false; onLibraryChanged(); }
    });
  }
})();
