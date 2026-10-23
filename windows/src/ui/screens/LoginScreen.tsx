import { useState } from "react";
import { loginWithConfig } from "../../core/api";
import {
  loadConfig,
  normalizeServerUrl,
  patchConfig,
} from "../../core/config";
import { useApp } from "../../core/store";
import "./Login.css";

export function LoginScreen({ onSuccess }: { onSuccess: () => void }) {
  const { toast } = useApp();
  const saved = loadConfig();
  const [serverUrl, setServerUrl] = useState(saved.serverUrl || "");
  const [username, setUsername] = useState(saved.username || "");
  const [password, setPassword] = useState(saved.password || "");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    const url = normalizeServerUrl(serverUrl);
    const user = username.trim();
    const pass = password;
    if (!url) {
      toast("请填写服务器地址", "error");
      return;
    }
    if (!user || !pass) {
      toast("请填写账号和密码", "error");
      return;
    }
    setBusy(true);
    try {
      const account = await loginWithConfig(url, user, pass);
      patchConfig({
        serverUrl: url,
        username: user,
        password: pass,
      });
      toast(account ? `已登录：${account.username}` : "已连接服务器", "success");
      onSuccess();
    } catch (e) {
      toast(e instanceof Error ? e.message : "登录失败", "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-brand">
          <div className="login-logo">剧</div>
          <h1 className="login-title">短剧库</h1>
          <p className="login-sub">连接服务器后进入剧库 · 配置会保存在本机</p>
        </div>

        <div className="field">
          <label htmlFor="login-server">服务器地址</label>
          <input
            id="login-server"
            value={serverUrl}
            placeholder="http://127.0.0.1:8998"
            onChange={(e) => setServerUrl(e.target.value)}
            spellCheck={false}
            disabled={busy}
          />
        </div>
        <div className="field">
          <label htmlFor="login-user">账号</label>
          <input
            id="login-user"
            value={username}
            placeholder="用户名"
            onChange={(e) => setUsername(e.target.value)}
            disabled={busy}
            autoComplete="username"
          />
        </div>
        <div className="field">
          <label htmlFor="login-pass">密码</label>
          <input
            id="login-pass"
            type="password"
            value={password}
            placeholder="密码"
            onChange={(e) => setPassword(e.target.value)}
            disabled={busy}
            autoComplete="current-password"
            onKeyDown={(e) => {
              if (e.key === "Enter") void submit();
            }}
          />
        </div>

        <button type="button" className="btn btn-primary login-submit" disabled={busy} onClick={() => void submit()}>
          {busy ? "正在连接…" : "进入剧库"}
        </button>
      </div>
    </div>
  );
}
