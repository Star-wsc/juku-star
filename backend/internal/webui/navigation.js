(() => {
  const pages = ['library', 'following', 'downloads', 'users'];
  const native = new WeakMap();
  let currentPage = pages.includes(location.hash.slice(1)) ? location.hash.slice(1) : 'library';
  let active = null, pendingBack = false, pendingOpen = null, pendingPage = '', pendingClose = '', listener = null;
  const state = (dialog = '', layer = '') => ({duanju: true, page: currentPage, dialog, layer});
  const url = () => location.pathname + location.search + '#' + currentPage;
  history.replaceState(state(), '', url());

  function backdropState() {
    document.body.classList.toggle('modal-open', Boolean(active));
    const toast = document.getElementById('messageText');
    if (toast) (active || document.body).appendChild(toast);
  }

  function finish(dialog) {
    if (dialog?.open) native.get(dialog).close();
    if (active === dialog) active = null;
    backdropState();
  }

  function open(dialog) {
    if (typeof dialog === 'string') dialog = document.getElementById(dialog);
    if (!dialog || dialog.open) return;
    if (active && history.state?.layer) close(active);
    const replace = Boolean(active);
    if (active) finish(active);
    active = dialog;
    native.get(dialog).show();
    backdropState();
    if (pendingBack) pendingOpen = dialog;
    else history[replace ? 'replaceState' : 'pushState'](state(dialog.id), '', url());
  }

  function close(dialog) {
    if (!dialog?.open) return;
    const managed = history.state?.duanju && history.state.dialog === dialog.id;
    const count = history.state?.layer ? 2 : 1;
    if (pendingOpen === dialog) pendingOpen = null;
    finish(dialog);
    if (managed && !pendingBack) {
      pendingBack = true;
      history.go(-count);
    } else if (managed && pendingBack) pendingClose = dialog.id;
  }

  function navigate(page) {
    if (!pages.includes(page)) return;
    if (active) close(active);
    if (pendingBack) {pendingPage = page; return;}
    if (page === currentPage) return;
    currentPage = page;
    history.pushState(state(), '', url());
    listener?.(currentPage);
  }

  function layer(name, visible) {
    if (!active) return;
    if (visible && history.state?.layer !== name) history[history.state?.layer ? 'replaceState' : 'pushState'](state(active.id, name), '', url());
    if (!visible && history.state?.layer === name && !pendingBack) {
      pendingBack = true;
      history.back();
    }
  }

  for (const dialog of document.querySelectorAll('dialog')) {
    native.set(dialog, {show: dialog.showModal.bind(dialog), close: dialog.close.bind(dialog)});
    dialog.showModal = () => open(dialog);
    dialog.close = () => close(dialog);
    dialog.addEventListener('cancel', event => {
      if (event.defaultPrevented) return;
      event.preventDefault();
      if (history.state?.layer) history.back(); else close(dialog);
    });
    dialog.addEventListener('click', event => {
      if (event.target !== dialog) return;
      const bounds = dialog.getBoundingClientRect();
      if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) close(dialog);
    });
  }
  document.querySelectorAll('[data-close]').forEach(button => button.addEventListener('click', () => close(button.closest('dialog'))));
  window.addEventListener('popstate', event => {
    const next = event.state || {};
    if (pendingBack) {
      pendingBack = false;
      if (pendingClose && next.dialog === pendingClose) {
        pendingClose = '';
        pendingBack = true;
        history.back();
        return;
      }
      pendingClose = '';
      if (pendingOpen?.open) {
        active = pendingOpen;
        pendingOpen = null;
        history.pushState(state(active.id), '', url());
        backdropState();
        return;
      }
      pendingOpen = null;
      if (pendingPage) {const page = pendingPage; pendingPage = ''; navigate(page); return;}
    }
    if (active && next.dialog !== active.id) finish(active);
    if (pages.includes(next.page)) currentPage = next.page;
    listener?.(currentPage);
    document.dispatchEvent(new CustomEvent('dialoglayerchange', {detail: next.layer || ''}));
    if (next.dialog && !active) history.replaceState(state(), '', url());
  });
  window.JukuDialogs = {open, close, navigate, layer, page: () => currentPage, listen: callback => {listener = callback;}};
})();
