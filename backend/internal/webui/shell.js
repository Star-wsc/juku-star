import { $, element, button, sourceLabel, normalizeSource, readPreference, savePreference, populateIcons } from './ui-core.js';

export function createShell(app) {
  const main = $('workspaceMain');
  const positions = {library: 0, following: 0, downloads: 0, users: 0};
  let current = '', sourceStates = {};

  function setPage(page) {
    if (!app.viewer && page === 'users') return;
    if (app.viewer && (page === 'users' && !app.viewer.account?.admin || page === 'downloads' && app.viewer.onlineOnly)) {
      window.JukuDialogs.navigate('library');
      page = 'library';
    }
    if (page === current) return;
    if (current) positions[current] = main.scrollTop;
    current = page;
    document.body.dataset.page = page;
    document.querySelectorAll('[data-view]').forEach(view => view.hidden = view.dataset.view !== page);
    document.querySelectorAll('button[data-page]').forEach(control => {
      if (control.dataset.page === page) control.setAttribute('aria-current', 'page');
      else control.removeAttribute('aria-current');
    });
    if (page === 'users') app.users?.load();
    if (page === 'following') app.following?.render();
    if (page === 'downloads') app.downloads?.render();
    if (page === 'library') app.library?.layout();
    requestAnimationFrame(() => {main.scrollTop = positions[page] || 0;});
  }

  function activity(visible) {
    visible = visible && !app.viewer?.onlineOnly;
    $('activityPanel').hidden = !visible;
    $('catalogLayout').classList.toggle('with-activity', visible);
    $('activityToggle').setAttribute('aria-pressed', String(visible));
    $('activityToggleLabel').textContent = visible ? '收起任务' : '任务侧栏';
    if (!app.viewer?.onlineOnly) savePreference('activitySidebar', visible);
    if (visible) app.downloads?.render();
  }

  function info(title, build) {
    $('infoTitle').textContent = title;
    $('infoContent').replaceChildren();
    if (typeof build === 'function') build($('infoContent'));
    else $('infoContent').appendChild(element('p', '', build));
    window.JukuDialogs.open('infoPanel');
  }

  function showSources() {
    const names = {cloudfront: '黄果旧 API', huangguoai: '黄果网页', 'huangguo-video': '黄果备用网页', huangdou: '黄豆', hongguo: '红果'};
    const statuses = {loading: '正在获取', ready: '已就绪', success: '已更新', done: '已更新', failed: '暂不可用', error: '暂不可用', cached: '使用缓存', pending: '等待更新'};
    info('站源状态', content => {
      for (const [id, label] of Object.entries(names)) {
        if (app.viewer?.sources && !app.viewer.sources.includes(normalizeSource(id))) continue;
        const state = sourceStates[id];
        const row = element('section', 'source-status-row');
        const heading = element('div', 'heading');
        heading.append(element('h3', '', label), element('span', 'tag', state ? statuses[state.status] || (state.error ? '暂不可用' : '已有缓存') : '未检查'));
        row.appendChild(heading);
        if (state) {
          const date = state.updatedAt && !state.updatedAt.startsWith('0001') ? new Date(state.updatedAt).toLocaleString() : '';
          row.appendChild(element('p', 'small', [Number.isFinite(state.count) ? state.count + ' 部' : '', date].filter(Boolean).join(' · ')));
          if (state.error) row.appendChild(element('p', 'error', state.error));
        }
        content.appendChild(row);
      }
      content.appendChild(element('p', 'small notice', '这里显示最近一次剧库请求的状态。点击“更新剧库”可重试，其他站源仍可独立使用。'));
    });
  }

  function positionMenu(menu) {
    const popover = menu.querySelector('.action-popover');
    if (!menu.open || !popover) return;
    popover.style.maxHeight = '';
    const anchor = menu.getBoundingClientRect();
    const viewport = main.getBoundingClientRect();
    const width = popover.getBoundingClientRect().width;
    const left = Math.max(12, Math.min(anchor.left, window.innerWidth - width - 12));
    const below = Math.max(0, Math.min(window.innerHeight, viewport.bottom) - anchor.bottom - 12);
    const above = Math.max(0, anchor.top - Math.max(0, viewport.top) - 12);
    const height = popover.scrollHeight + 2;
    const upward = height > below && above > below;
    popover.style.left = left - anchor.left + 'px';
    popover.style.right = 'auto';
    popover.style.maxHeight = Math.max(80, upward ? above : below) + 'px';
    popover.style.overflowY = 'auto';
    popover.style.top = upward ? -(Math.min(height, above) + 6) + 'px' : anchor.height + 6 + 'px';
  }

  function access() {
    document.body.classList.toggle('online-only', Boolean(app.viewer?.onlineOnly));
    document.querySelectorAll('[data-admin-only]').forEach(control => {control.hidden = !app.viewer?.account?.admin;});
    document.querySelectorAll('button[data-page=downloads], [data-go=downloads]').forEach(control => {control.hidden = Boolean(app.viewer?.onlineOnly);});
    activity(readPreference('activitySidebar', false) === true);
    setPage(window.JukuDialogs.page());
  }

  function init() {
    populateIcons();
    window.JukuDialogs.listen(setPage);
    document.querySelectorAll('button[data-page], [data-go]').forEach(control => control.addEventListener('click', () => {
      if (control.dataset.go === 'following') app.following.showTab('watching');
      window.JukuDialogs.navigate(control.dataset.page || control.dataset.go);
    }));
    document.querySelector('.brand').addEventListener('click', event => {event.preventDefault(); window.JukuDialogs.navigate('library');});
    document.querySelector('.skip-link').addEventListener('click', event => {event.preventDefault(); main.focus();});
    $('headerMenu').addEventListener('click', event => {
      if (event.target.closest('button')) {
        $('headerMenu').open = false;
        $('moreButton').focus({preventScroll: true});
      }
    }, true);
    $('headerMenu').addEventListener('toggle', () => $('moreButton').setAttribute('aria-expanded', String($('headerMenu').open)));
    $('menuHistoryBtn').addEventListener('click', () => $('openHistoryBtn').click());
    $('sourceStatusBtn').addEventListener('click', showSources);
    $('aboutBtn').addEventListener('click', () => info('使用提示', content => {
      for (const text of ['输入关键词先筛选本地剧库，点击“联网搜索”补充红果结果。', '更新剧库会查新、继续加载目录，并在后台补齐历史资料。', '登录账号后，观看进度和追剧清单可跨设备接续；允许匿名访问时，匿名记录仍按浏览器独立保存。', '下载保存到运行短剧库的电脑；手机浏览器不会把下载任务保存到手机相册。']) content.appendChild(element('p', 'notice', text));
    }));
    $('activityToggle').addEventListener('click', () => activity($('activityPanel').hidden));
    $('closeActivityBtn').addEventListener('click', () => activity(false));
    $('downloadLocationBtn').addEventListener('click', () => {
      window.JukuDialogs.open('settingsPanel');
      $('downloadDirectory').focus();
    });
    document.addEventListener('click', event => {
      for (const menu of document.querySelectorAll('.action-menu[open]')) {
        if (!menu.contains(event.target) || event.target.closest('button')) menu.open = false;
      }
    });
    document.addEventListener('toggle', event => {
      if (event.target.matches?.('.action-menu')) positionMenu(event.target);
    }, true);
    document.addEventListener('keydown', event => {
      if (event.key !== 'Escape' || document.querySelector('dialog[open]')) return;
      const menu = document.querySelector('.action-menu[open]');
      if (!menu) return;
      event.preventDefault();
      menu.open = false;
      menu.querySelector('summary').focus({preventScroll: true});
    });
    const positionMenus = () => document.querySelectorAll('.action-menu[open]').forEach(positionMenu);
    window.addEventListener('resize', positionMenus);
    main.addEventListener('scroll', positionMenus, {passive: true});
    activity(readPreference('activitySidebar', false) === true);
    setPage(window.JukuDialogs.page());
  }

  return {init, access, info, page: () => current, resetScroll: () => {main.scrollTop = 0;}, sourceStates: value => {sourceStates = value || {};}};
}
