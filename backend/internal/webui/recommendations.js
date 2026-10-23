import { $, api } from './ui-core.js';

export function createRecommendations({merge, changed, switched, allowed = () => true}) {
  const feeds = new Map();
  let enabled = false, genre = 'short_play', controller, sequence = 0, busy = false;
  function current() {
    if (!feeds.has(genre)) feeds.set(genre, {items: [], offset: 0, sessionId: '', hasMore: true, loaded: false, error: '', saved: true});
    return feeds.get(genre);
  }
  function cancel() {
    sequence++;
    controller?.abort();
    controller = null;
    busy = false;
  }
  function render() {
    const feed = current();
    $('recommendFooter').hidden = !enabled;
    $('recommendMoreBtn').hidden = !feed.hasMore && !feed.error;
    $('recommendMoreBtn').disabled = busy;
    $('recommendMoreBtn').textContent = busy ? '读取中…' : feed.error ? '重试' : feed.loaded ? '继续推荐' : '获取推荐';
    $('recommendRefreshBtn').disabled = busy;
    $('recommendRefreshBtn').setAttribute('aria-busy', String(busy));
    $('recommendStatus').textContent = busy ? '正在读取红果分类推荐…' : feed.error || (feed.loaded ? '已显示 ' + feed.items.length + ' 部' + (feed.hasMore ? '，按红果默认推荐顺序' : '，本轮已读完') + (feed.saved ? '' : '；缓存暂未保存') : '选择分类获取推荐');
    $('recommendStatus').classList.toggle('error', Boolean(feed.error));
  }
  async function load(reset = Boolean(current().retryReset)) {
    if (!allowed() || !enabled || busy) return;
    const feed = current();
    if (!reset && !feed.hasMore && !feed.error) return;
    cancel();
    const token = sequence, requestGenre = genre;
    controller = new AbortController();
    const signal = controller.signal;
    busy = true;
    feed.error = '';
    render();
    try {
      const result = await api('/api/ui/recommendations', {
        method: 'POST', signal,
        body: JSON.stringify({genre, offset: reset ? 0 : feed.offset, sessionId: reset ? '' : feed.sessionId, seen: reset ? [] : feed.items.slice(-540).map(item => item.id)})
      });
      if (signal.aborted || token !== sequence || genre !== requestGenre || !enabled) return;
      if (!Array.isArray(result.data) || typeof result.hasMore !== 'boolean' || !Number.isInteger(result.nextOffset) || typeof result.sessionId !== 'string') throw new Error('推荐数据格式无效');
      const known = new Set(reset ? [] : feed.items.map(item => item.id));
      const rows = result.data.filter(item => /^hongguo:[0-9]{1,32}$/.test(item.id) && item.source === 'hongguo').filter(item => {
        if (known.has(item.id)) return false;
        known.add(item.id);
        return true;
      });
      if (result.hasMore && (result.nextOffset <= (reset ? 0 : feed.offset) || !rows.length)) throw new Error('推荐分页未前进，请重新获取');
      feed.items = reset ? rows : feed.items.concat(rows);
      feed.offset = result.nextOffset;
      feed.sessionId = result.sessionId;
      feed.hasMore = result.hasMore;
      feed.loaded = true;
      feed.retryReset = false;
      feed.saved = result.saved !== false;
      merge(rows);
      changed();
    } catch (error) {
      if (token === sequence && !signal.aborted) {feed.error = error.message; feed.retryReset = reset;}
    } finally {
      if (token === sequence) {
        busy = false;
        controller = null;
        render();
      }
    }
  }
  function setEnabled(value) {
    if (value && !allowed()) return;
    cancel();
    enabled = value;
    $('librarySearch').hidden = enabled;
    $('libraryFilters').hidden = enabled;
    $('recommendToolbar').hidden = !enabled;
    $('recentSearches').hidden = true;
    $('libraryModeBtn').setAttribute('aria-pressed', String(!enabled));
    $('recommendModeBtn').setAttribute('aria-pressed', String(enabled));
    document.body.classList.toggle('show-recommendations', enabled);
    switched();
    render();
    if (enabled && !current().loaded) load();
  }
  function init() {
    $('recommendModeBtn').hidden = !allowed();
    $('libraryModeBtn').addEventListener('click', () => setEnabled(false));
    $('recommendModeBtn').addEventListener('click', () => setEnabled(true));
    $('recommendGenre').addEventListener('change', () => {
      cancel();
      genre = $('recommendGenre').value;
      changed();
      render();
      if (!current().loaded) load();
    });
    $('recommendMoreBtn').addEventListener('click', () => load());
    $('recommendRefreshBtn').addEventListener('click', () => load(true));
  }
  return {init, get enabled() {return enabled;}, items: () => current().items, all: () => Array.from(feeds.values()).flatMap(feed => feed.items)};
}
