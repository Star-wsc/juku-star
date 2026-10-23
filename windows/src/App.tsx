import { useEffect, useState } from "react";
import {
  ensureViewer,
  loginWithConfig,
  logout as apiLogout,
  setBaseUrl,
  setToast,
} from "./core/api";
import {
  hasCredentials,
  loadConfig,
  normalizeServerUrl,
  patchConfig,
} from "./core/config";
import { AppProvider, useApp } from "./core/store";
import { AppShell } from "./ui/layout/AppShell";
import { TitleBar } from "./ui/layout/TitleBar";
import { LibraryScreen } from "./ui/screens/LibraryScreen";
import { DetailScreen } from "./ui/screens/DetailScreen";
import { PlayerScreen } from "./ui/screens/PlayerScreen";
import { SettingsScreen } from "./ui/screens/SettingsScreen";
import { LoginScreen } from "./ui/screens/LoginScreen";
import {
  DownloadScreen,
  FollowingScreen,
  HistoryScreen,
} from "./ui/screens/MoreScreens";
import "./ui/theme/global.css";

function Router() {
  const { phase, setPhase, page, setPage, toast, serverUrl, setServerUrl, setAccountName, accountName } =
    useApp();
  const [booting, setBooting] = useState(true);

  useEffect(() => {
    setToast(toast);
  }, [toast]);

  // 启动：有保存的配置则自动登录；否则停在登录门
  useEffect(() => {
    const config = loadConfig();
    document.documentElement.setAttribute("data-theme", config.theme);
    if (!hasCredentials()) {
      setBooting(false);
      setPhase("login");
      return;
    }
    const url = normalizeServerUrl(config.serverUrl);
    setBaseUrl(url);
    setServerUrl(url);
    void (async () => {
      try {
        const account = await loginWithConfig(url, config.username, config.password);
        setAccountName(account?.username || config.username);
        setPhase("app");
      } catch {
        setPhase("login");
      } finally {
        setBooting(false);
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onLogout = async () => {
    try {
      await apiLogout();
    } catch {
      /* ignore */
    }
    setAccountName(null);
    setPhase("login");
    setPage("library");
    toast("已退出登录，可重新配置服务器", "success");
  };

  const onEnterApp = async () => {
    const config = loadConfig();
    const url = normalizeServerUrl(config.serverUrl);
    setBaseUrl(url);
    setServerUrl(url);
    try {
      const v = await ensureViewer();
      setAccountName(v.account?.username || config.username || null);
    } catch {
      setAccountName(config.username || null);
    }
    setPage("library");
    setPhase("app");
  };

  if (booting) {
    return (
      <div className="window-frame">
        <TitleBar title="短剧库" />
        <div className="content-area" style={{ justifyContent: "center", alignItems: "center" }}>
          <div className="empty-state">
            <div className="empty-icon">◎</div>
            <div>正在启动…</div>
          </div>
        </div>
      </div>
    );
  }

  if (phase === "login") {
    return (
      <div className="window-frame">
        <TitleBar title="短剧库 · 连接服务器" />
        <LoginScreen onSuccess={() => void onEnterApp()} />
      </div>
    );
  }

  const pageTitle =
    page === "library"
      ? "短剧库"
      : page === "following"
        ? "追剧"
        : page === "download"
          ? "下载"
          : page === "history"
            ? "历史"
            : page === "settings"
              ? "设置"
              : page === "detail"
                ? "详情"
                : page === "player"
                  ? "播放"
                  : "短剧库";

  return (
    <AppShell
      title={pageTitle}
      active={page}
      onSelect={setPage}
      serverLabel={serverUrl.replace(/^https?:\/\//, "") || "未连接"}
      accountName={accountName}
    >
      {page === "library" ? (
        <LibraryScreen />
      ) : page === "following" ? (
        <FollowingScreen />
      ) : page === "download" ? (
        <DownloadScreen />
      ) : page === "history" ? (
        <HistoryScreen />
      ) : page === "settings" ? (
        <SettingsScreen onLogout={() => void onLogout()} />
      ) : page === "detail" ? (
        <DetailScreen />
      ) : page === "player" ? (
        <PlayerScreen />
      ) : (
        <LibraryScreen />
      )}
    </AppShell>
  );
}

export default function App() {
  return (
    <AppProvider>
      <Router />
    </AppProvider>
  );
}

// 重新配置服务器：回到登录门（保留已填配置）
export function reconfigure() {
  patchConfig({});
}
