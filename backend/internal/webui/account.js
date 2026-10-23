import { $, post, setMessage } from './ui-core.js';
import { initializeViewer, announceViewerChange } from './viewer.js';

export function createAccount(app) {
  let mode = 'login', busy = false;
  const panel = $('accountPanel');

  function setMode(value) {
    if (busy) return;
    mode = value === 'register' && app.viewer?.allowRegistration !== false ? 'register' : 'login';
    const registering = mode === 'register';
    document.querySelectorAll('[data-account-mode]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.accountMode === mode)));
    $('accountConfirmRow').hidden = !registering;
    $('accountPasswordConfirm').required = registering;
    $('accountImportOnRegisterRow').hidden = !registering;
    $('accountPassword').autocomplete = registering ? 'new-password' : 'current-password';
    $('accountSubmitBtn').textContent = registering ? '注册并登录' : '登录';
    $('accountStatus').textContent = '';
  }

  function setBusy(value) {
    busy = value;
    panel.querySelectorAll('input, button:not([data-close])').forEach(control => {control.disabled = value;});
    panel.setAttribute('aria-busy', String(value));
  }

  function finish(message, error = false) {
    try { sessionStorage.setItem('juku.account.flash', JSON.stringify({message, error})); } catch (_) {}
    announceViewerChange();
    location.reload();
  }

  async function authenticate(event) {
    event.preventDefault();
    if (busy) return;
    const registering = mode === 'register';
    if (registering && $('accountPassword').value !== $('accountPasswordConfirm').value) {
      $('accountStatus').textContent = '两次输入的密码不一致';
      return;
    }
    const importRecords = registering && $('accountImportOnRegister').checked;
    const username = $('accountUsername').value.trim(), password = $('accountPassword').value;
    setBusy(true);
    $('accountStatus').textContent = registering ? '正在创建账号…' : '正在登录…';
    try {
      await post('/api/ui/account/' + mode, {username, password});
      $('accountPassword').value = '';
      $('accountPasswordConfirm').value = '';
      app.viewer = await initializeViewer();
      if (!app.viewer.account) throw new Error('登录 Cookie 未保存，请允许此网站保存 Cookie 后重试');
      let message = registering ? '账号已创建，观看记录将跟随账号保存。' : '已登录 ' + app.viewer.account.username + '，个人记录已加载。';
      if (importRecords && app.viewer.guestImportAvailable) {
        try {
          await post('/api/ui/account/import', {});
          message = '账号已创建，此浏览器原有记录已接入账号。';
        } catch (error) {
          finish('账号已创建；' + error.message + '。可在账号面板重试接回记录。', true);
          return;
        }
      }
      finish(message);
    } catch (error) {
      $('accountStatus').textContent = error.message;
      if (app.viewer?.account) finish('账号已登录，请刷新后继续。', true);
    } finally { setBusy(false); }
  }

  async function logout() {
    if (busy) return;
    setBusy(true);
    $('accountStatus').textContent = '正在退出…';
    try {
      await post('/api/ui/account/logout', {});
      finish('已退出账号。账号记录已保留，下次登录可继续。');
    } catch (error) { $('accountStatus').textContent = error.message; }
    finally { setBusy(false); }
  }

  async function changePassword(event) {
    event.preventDefault();
    if (busy) return;
    if ($('accountNewPassword').value !== $('accountNewPasswordConfirm').value) {
      $('accountStatus').textContent = '两次输入的新密码不一致';
      return;
    }
    const password = $('accountCurrentPassword').value, newPassword = $('accountNewPassword').value;
    setBusy(true);
    $('accountStatus').textContent = '正在修改密码…';
    try {
      await post('/api/ui/account/password', {password, newPassword});
      $('accountPasswordForm').reset();
      finish('密码已修改，其他设备的登录已失效。');
    } catch (error) { $('accountStatus').textContent = error.message; }
    finally { setBusy(false); }
  }

  async function importGuest() {
    if (busy) return;
    setBusy(true);
    $('accountStatus').textContent = '正在接回此浏览器原有记录…';
    try {
      await post('/api/ui/account/import', {});
      finish('此浏览器原有记录已接入账号，原始文件仍保留。');
    } catch (error) { $('accountStatus').textContent = error.message; }
    finally { setBusy(false); }
  }

  function open() {
    $('headerMenu').open = false;
    window.JukuDialogs.open(panel);
  }

  function init() {
    const account = app.viewer?.account;
    const forcedPassword = Boolean(account?.requirePasswordChange);
    const loginPage = location.pathname === '/login' || forcedPassword || app.viewer?.requireLogin && !account;
    $('accountGuest').hidden = Boolean(account);
    $('accountMember').hidden = !account;
    $('accountLabel').textContent = account?.username || '登录';
    $('openAccountBtn').setAttribute('aria-label', account ? '账号：' + account.username : '登录或注册账号');
    $('openAccountBtn').title = account ? '当前账号：' + account.username : '登录后跨设备接着看';
    $('accountName').textContent = account?.username || '';
    $('accountGuestImportSection').hidden = !app.viewer?.guestImportAvailable;
    $('accountManageUsersBtn').hidden = !account?.admin || forcedPassword;
    $('accountRegistrationDisabled').hidden = app.viewer?.allowRegistration !== false;
    document.querySelector('[data-account-mode="register"]').hidden = app.viewer?.allowRegistration === false;
    $('accountForcedPasswordHint').hidden = !forcedPassword;
    if (forcedPassword) {
      $('accountPasswordDetails').open = true;
      $('accountPasswordDetails').querySelector('summary').hidden = true;
      $('accountGuestImportSection').hidden = true;
    }
    $('viewerIdentityLabel').textContent = account ? '账号：' + account.username : '当前浏览器';
    $('viewerRecordsHint').textContent = account ? '观看进度和追剧清单跟随账号保存，换设备登录同一账号即可继续。可用站源和下载权限由管理员设置。' : '观看进度和追剧清单保存在当前浏览器身份下。点击顶栏的账号图标注册或登录，可跨设备继续观看。';
    const searchLabel = account ? '搜索我的观看记录' : '搜索本浏览器的观看记录';
    $('historySearch').placeholder = searchLabel;
    $('historySearch').setAttribute('aria-label', searchLabel);
    $('openAccountBtn').addEventListener('click', open);
    document.querySelectorAll('[data-account-open]').forEach(button => button.addEventListener('click', open));
    document.querySelectorAll('[data-account-mode]').forEach(button => button.addEventListener('click', () => setMode(button.dataset.accountMode)));
    $('accountForm').addEventListener('submit', authenticate);
    $('accountLogoutBtn').addEventListener('click', logout);
    $('accountPasswordForm').addEventListener('submit', changePassword);
    $('accountImportGuestBtn').addEventListener('click', importGuest);
    panel.addEventListener('close', () => panel.querySelectorAll('input[type=password]').forEach(input => {input.value = '';}));
    setMode('login');
    if (loginPage) {
      document.body.classList.add('account-page');
      panel.setAttribute('open', '');
      panel.querySelector('[data-close]').hidden = true;
      $('accountTitle').textContent = forcedPassword ? '修改初始密码' : '登录剧库';
      $('accountAnonymousLink').hidden = app.viewer?.requireLogin || Boolean(account);
      document.documentElement.dataset.ready = 'true';
    }
    try {
      const flash = JSON.parse(sessionStorage.getItem('juku.account.flash') || 'null');
      sessionStorage.removeItem('juku.account.flash');
      if (flash?.message) setMessage(flash.message, flash.error);
      else if (app.viewer?.accountExpired) setMessage('登录已过期，请重新登录账号接着看。');
    } catch (_) {}
    return Boolean(loginPage);
  }

  return {init};
}
