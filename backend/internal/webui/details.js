import { $, element, button, icon, coverURL, withFocus, dramaTitle, sourceKey, sourceLabel, categoryName, episodeCount, firstNonEmpty, tagsText, releaseText, setMessage } from './ui-core.js';

export function createDetails(app) {
  let currentID = '';
  const metadataMessages = new Map();
  let poster;
  const exporting = new Set();
  const panel = $('detailPanel');
  const content = $('detailContent');

  function renderCover(drama) {
    const address = coverURL(drama), title = dramaTitle(drama);
    if (poster?.id === drama.id && poster.address === address) {
      if (poster.image) poster.image.alt = title + ' 海报';
      return poster.node;
    }
    const node = element('div', 'detail-cover');
    const status = element('span', 'detail-cover-status', address ? '加载海报…' : '暂无海报');
    status.setAttribute('role', 'status');
    node.appendChild(status);
    const current = {id: drama.id, address, node, failed: false};
    poster = current;
    if (!address) return node;
    const image = element('img');
    current.image = image;
    image.alt = title + ' 海报';
    image.decoding = 'async';
    image.hidden = true;
    image.addEventListener('load', () => {
      if (poster !== current) return;
      current.failed = false;
      image.hidden = false;
      status.hidden = true;
      app.library.coverLoaded(drama.id, address);
    });
    image.addEventListener('error', () => {
      if (poster !== current) return;
      current.failed = true;
      image.hidden = true;
      status.hidden = false;
      status.textContent = '海报加载失败';
      app.library.coverFailed(drama.id, address);
    });
    current.retry = () => {
      if (!current.failed) return;
      current.failed = false;
      status.textContent = '加载海报…';
      image.src = address;
    };
    node.appendChild(image);
    image.src = address;
    return node;
  }

  function refreshCover(id, retry = false) {
    if (!panel.open || currentID !== id) return;
    const drama = app.library.get(id);
    if (!drama) return;
    const previous = poster?.node;
    const next = renderCover(drama);
    if (previous !== next) previous?.replaceWith(next);
    if (retry) poster?.retry?.();
  }

  function render() {
    if (!currentID) return;
    const saved = app.following.get(currentID);
    const history = window.JukuHistory.get(currentID);
    const known = app.library.get(currentID);
    const drama = known || {id: currentID, title: saved?.title || history?.title || '短剧', source: saved?.source || history?.source, totalEpisode: saved?.totalEpisode || history?.total, categoryName: saved?.category};
    const title = dramaTitle(drama);
    withFocus(content, () => {
      content.replaceChildren();
      const top = element('div', 'detail-top');
      const heading = element('div', 'spacer');
      const name = element('h1', '', title);
      name.id = 'detailTitle';
      heading.append(name, element('p', 'detail-meta', sourceLabel(sourceKey(drama)) + ' · ' + categoryName(drama)));
      const tags = element('div', 'tags detail-tags');
      tagsText(drama).slice(0, 10).forEach(tag => tags.appendChild(element('span', 'tag', tag)));
      if (drama.vip === true) tags.prepend(element('span', 'tag vip-tag', 'VIP · 仅试看'));
      heading.appendChild(tags);
      top.append(renderCover(drama), heading);
      content.appendChild(top);
      const metadataStatus = element('p', 'small', metadataMessages.get(currentID) || '');
      metadataStatus.id = 'detailMetadataStatus';
      metadataStatus.setAttribute('role', 'status');
      metadataStatus.hidden = !metadataStatus.textContent;
      content.appendChild(metadataStatus);
      if (drama.vip === true) content.appendChild(element('p', 'notice vip-notice', '此剧含 VIP 分集，站源仅提供试看；试看内容不会作为完整分集下载。'));
      if (history) content.appendChild(element('p', 'detail-progress', window.JukuHistory.progressText(history)));
      if (saved?.newEpisodes > 0) content.appendChild(element('p', 'following-update notice', '剧库新增 ' + saved.newEpisodes + ' 集'));
      const actions = element('div', 'detail-actions');
      const play = button(history ? '继续观看' : '开始观看', () => app.play(currentID, title), false, 'primary-action button-content');
      play.prepend(icon('play'));
      play.dataset.focusKey = 'detail-play';
      const bookmark = button(saved?.saved ? '已收藏' : '加入想看', () => app.following.toggleSaved(currentID), app.following.busy(currentID), 'secondary');
      bookmark.id = 'detailSaveBtn';
      bookmark.dataset.focusKey = 'detail-save';
      bookmark.setAttribute('aria-pressed', String(Boolean(saved?.saved)));
      actions.append(play, bookmark);
      content.appendChild(actions);
      content.appendChild(element('p', 'detail-description', firstNonEmpty(drama.desc, drama.intro) || '站源暂未提供简介。'));
      const facts = element('dl', 'detail-facts');
      const values = [['集数', episodeCount(drama) ? episodeCount(drama) + ' 集' : '暂未提供'], ['状态', releaseText(drama.releaseStatus)], ['上线时间', drama.onlineDate], ['站点热度', drama.heat], ['播放量', drama.views], ['站点评分', drama.score]];
      for (const [label, value] of values) {
        if (!value) continue;
        const item = element('div');
        item.append(element('dt', '', label), element('dd', '', value));
        facts.appendChild(item);
      }
      content.appendChild(facts);
      const secondary = element('div', 'detail-secondary');
      const download = button('加入下载', () => app.downloads.enqueue([currentID]), !known, 'secondary');
      download.id = 'detailDownloadBtn';
      download.dataset.focusKey = 'detail-download';
      if (!known) download.title = '更新剧库找到此剧后可加入下载';
      const watched = button(saved?.completed ? '取消已看标记' : '标为已看', () => app.following.setCompleted(currentID, !saved?.completed), app.following.busy(currentID), 'secondary');
      watched.id = 'detailCompletedBtn';
      watched.dataset.focusKey = 'detail-completed';
      const emby = button(exporting.has(currentID) ? '导出中…' : '导出 Emby', () => exportEmby(currentID, title), !known || exporting.has(currentID), 'secondary');
      emby.id = 'detailEmbyBtn';
      emby.dataset.focusKey = 'detail-emby';
      emby.title = '导出 STRM 分集文件，用于现有 Emby 电视剧媒体库';
      if (app.viewer?.onlineOnly) secondary.appendChild(watched);
      else secondary.append(app.downloads.qualityControl(), download, watched, emby);
      actions.after(secondary);
      content.appendChild(element('p', 'small notice', '手动标记用于整理清单，实际播放进度仍自动保存。'));
    });
  }

  async function exportEmby(id, title) {
    if (exporting.has(id)) return;
    exporting.add(id);
    render();
    try {
      const response = await fetch('/api/emby/export', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({dramaId: id, baseUrl: location.origin})});
      if (!response.ok) {const result = await response.json(); throw new Error(result.error || '导出失败');}
      const address = URL.createObjectURL(await response.blob());
      const link = document.createElement('a');
      link.href = address;
      link.download = title + '-Emby.zip';
      document.body.appendChild(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(address), 60000);
      setMessage('已导出 Emby 分集；解压到 Emby 电视剧媒体库后扫描即可。');
    } catch (error) {setMessage('Emby 导出失败：' + error.message, true);}
    finally {exporting.delete(id); if (panel.open && currentID === id) render();}
  }

  function open(id) {
    currentID = id;
    render();
    window.JukuDialogs.open(panel);
    app.library.refreshDrama(id);
  }

  function metadataStatus(id, text) {
    metadataMessages.set(id, text);
    const status = $('detailMetadataStatus');
    if (panel.open && currentID === id && status) {status.textContent = text; status.hidden = !text;}
  }

  return {open, metadataStatus, refreshCover, retryCover: () => refreshCover(currentID, true), refresh: () => {if (panel.open) render();}};
}
