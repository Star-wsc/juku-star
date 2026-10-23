import { $, api, post, element } from './ui-core.js';

export function createUsers(app) {
  let adminLoading = false, creating = false, saving = false;

  function sourceChoices(container, selected, disabled = false) {
    container.replaceChildren();
    const choices = app.viewer.sourceChoices || [];
    for (const choice of choices) {
      const label = element('label', 'account-source-choice');
      const checkbox = element('input');
      checkbox.type = 'checkbox';
      checkbox.value = choice.id;
      checkbox.checked = selected.includes(choice.id);
      checkbox.disabled = disabled;
      label.append(checkbox, element('span', '', choice.name));
      container.appendChild(label);
    }
  }

  function selectedSources(container) {
    return Array.from(container.querySelectorAll('input:checked'), input => input.value);
  }

  function filterAccounts() {
    const keyword = $('adminAccountSearch').value.trim().toLocaleLowerCase();
    let shown = 0;
    for (const row of $('adminAccountList').children) {
      row.hidden = !row.dataset.username.toLocaleLowerCase().includes(keyword);
      if (!row.hidden) shown++;
    }
    $('adminAccountEmpty').hidden = shown > 0;
  }

  function renderAccounts(accounts) {
    $('adminAccountCount').textContent = accounts.length + ' 个账号';
    const rows = accounts.map(account => {
      const row = element('li', 'admin-account-row');
      row.dataset.username = account.username;
      const heading = element('div', 'admin-account-heading');
      const role = element('span', 'small', account.admin ? '管理员 · 全部权限' : account.onlineOnly ? '仅在线观看' : '可观看和下载');
      heading.append(element('strong', '', account.username), role);
      const permissions = element('div', 'admin-account-permissions');
      const choices = element('div', 'account-source-choices');
      choices.setAttribute('role', 'group');
      choices.setAttribute('aria-label', account.username + ' 的站源权限');
      sourceChoices(choices, account.sources || [], account.admin);
      permissions.appendChild(choices);
      row.append(heading, permissions);
      if (!account.admin) {
        const onlyLabel = element('label', 'account-check');
        const only = element('input');
        only.type = 'checkbox';
        only.className = 'account-online-only';
        only.checked = account.onlineOnly === true;
        onlyLabel.append(only, element('span', '', '仅在线观看'));
        permissions.appendChild(onlyLabel);
        const save = element('button', 'secondary', '保存权限');
        save.type = 'button';
        save.disabled = true;
        permissions.addEventListener('change', () => {
          const selected = selectedSources(choices);
          save.disabled = !selected.length || selected.join(',') === account.sources.join(',') && only.checked === account.onlineOnly;
        });
        save.addEventListener('click', async () => {
          save.disabled = true;
          permissions.querySelectorAll('input').forEach(input => input.disabled = true);
          try {
            const result = await post('/api/ui/admin/accounts/permissions', {username: account.username, sources: selectedSources(choices), onlineOnly: only.checked});
            account = result.account;
            sourceChoices(choices, account.sources);
            only.checked = account.onlineOnly;
            role.textContent = account.onlineOnly ? '仅在线观看' : '可观看和下载';
            $('adminStatus').textContent = '已更新 ' + account.username + ' 的权限，现有登录立即生效。';
          } catch (error) {
            save.disabled = false;
            $('adminStatus').textContent = error.message;
          } finally {permissions.querySelectorAll('input').forEach(input => input.disabled = false);}
        });
        row.appendChild(save);
      }
      return row;
    });
    $('adminAccountList').replaceChildren(...rows);
    filterAccounts();
  }

  async function loadAdmin() {
    if (!app.viewer?.account?.admin || app.viewer.account.requirePasswordChange || adminLoading) return;
    adminLoading = true;
    $('adminRefreshBtn').disabled = true;
    $('adminSavePolicyBtn').disabled = true;
    try {
      const [policy, accounts] = await Promise.all([api('/api/ui/admin/settings'), api('/api/ui/admin/accounts')]);
      $('adminRequireLogin').checked = policy.requireLogin;
      $('adminAllowRegistration').checked = policy.allowRegistration;
      $('adminSavePolicyBtn').disabled = false;
      if (accounts.sourceChoices) app.viewer.sourceChoices = accounts.sourceChoices;
      sourceChoices($('adminNewUserSources'), (app.viewer.sourceChoices || []).map(choice => choice.id));
      renderAccounts(accounts.data || []);
      $('adminStatus').textContent = '';
    } catch (error) { $('adminStatus').textContent = error.message; }
    finally { adminLoading = false; $('adminRefreshBtn').disabled = false; }
  }

  async function savePolicy(event) {
    event.preventDefault();
    if (saving || adminLoading) return;
    saving = true;
    $('adminSavePolicyBtn').disabled = true;
    $('adminStatus').textContent = '正在保存访问设置…';
    try {
      const policy = await post('/api/ui/admin/settings', {requireLogin: $('adminRequireLogin').checked, allowRegistration: $('adminAllowRegistration').checked});
      Object.assign(app.viewer, policy);
      $('adminStatus').textContent = '访问设置已保存并生效。';
    } catch (error) { $('adminStatus').textContent = error.message; }
    finally { saving = false; $('adminSavePolicyBtn').disabled = false; }
  }

  async function createUser(event) {
    event.preventDefault();
    if (creating || adminLoading) return;
    creating = true;
    $('adminCreateUserBtn').disabled = true;
    try {
      const username = $('adminNewUsername').value.trim();
      const sources = selectedSources($('adminNewUserSources'));
      if (!sources.length) throw new Error('请至少选择一个可用站源');
      await post('/api/ui/admin/accounts', {username, password: $('adminNewUserPassword').value, sources, onlineOnly: $('adminNewOnlineOnly').checked});
      $('adminCreateUserForm').reset();
      await loadAdmin();
      $('adminStatus').textContent = '已创建用户 ' + username + '，可以登录此剧库。';
    } catch (error) { $('adminStatus').textContent = error.message; }
    finally { creating = false; $('adminCreateUserBtn').disabled = false; }
  }

  function init() {
    if (!app.viewer?.account?.admin) return;
    $('adminPolicyForm').addEventListener('submit', savePolicy);
    $('adminCreateUserForm').addEventListener('submit', createUser);
    $('adminRefreshBtn').addEventListener('click', loadAdmin);
    $('adminAccountSearch').addEventListener('input', filterAccounts);
  }

  return {init, load: loadAdmin};
}
