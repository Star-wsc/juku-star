import { useEffect, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { loadConfig, patchConfig, type AppConfig } from "../../core/config";
import { useApp } from "../../core/store";
import { Chip, SwitchRow } from "../components";
import "./Settings.css";

interface Props {
  onLogout: () => void;
}

export function SettingsScreen({ onLogout }: Props) {
  const { serverUrl, accountName, setPhase, setPage } = useApp();
  const [config, setConfig] = useState<AppConfig>(() => loadConfig());
  const [version, setVersion] = useState("0.1.0");

  useEffect(() => {
    void invoke<string>("app_version")
      .then(setVersion)
      .catch(() => {});
  }, []);

  const patch = (p: Partial<AppConfig>) => {
    const next = patchConfig(p);
    setConfig(next);
    document.documentElement.setAttribute("data-theme", next.theme);
    window.dispatchEvent(new Event("juku-config-changed"));
  };

  return (
    <div className="content-scroll">
      <h1 className="t-page-title">设置</h1>
      <p className="t-page-sub">外观与播放偏好 · 开关一律靠右</p>

      <div style={{ height: 22 }} />

      {/* 账号信息展示 + 退出；服务器只能在登录页配置 */}
      <div className="settings-card">
        <div className="settings-card-title">账号</div>
        <div className="settings-account-row">
          <div>
            <div className="switch-title">{accountName || config.username || "未登录"}</div>
            <div className="switch-desc">
              服务器 {serverUrl.replace(/^https?:\/\//, "") || config.serverUrl || "—"}
            </div>
            <div className="switch-desc">观看记录与追剧绑定该账号</div>
          </div>
          <div style={{ display: "flex", gap: 10 }}>
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => {
                setPhase("login");
                setPage("library");
              }}
            >
              重新配置服务器
            </button>
            <button type="button" className="btn btn-primary" onClick={onLogout}>
              退出账号登录
            </button>
          </div>
        </div>
      </div>

      {/* 双黄蛋：内容源 */}
      <div className="settings-card">
        <div className="settings-card-title">内容源</div>
        <p className="settings-hint">控制剧库显示哪些站源，关闭后该剧集不再展示。红果始终显示。</p>
        <SwitchRow
          title="黄果"
          description="黄果视频、黄果原创等（含 cloudfront）"
          checked={config.showHuangguo}
          onChange={(v) => patch({ showHuangguo: v })}
        />
        <div className="settings-divider" />
        <SwitchRow
          title="黄豆"
          description="黄豆及其子源"
          checked={config.showHuangdou}
          onChange={(v) => patch({ showHuangdou: v })}
        />
      </div>

      <div className="settings-card">
        <div className="settings-card-title">外观</div>
        <SwitchRow
          title="深色模式"
          description="浅色 / 深色"
          checked={config.theme === "dark"}
          onChange={(v) => patch({ theme: v ? "dark" : "light" })}
        />
        <div className="settings-divider" />
        <div className="theme-chips">
          <Chip selected={config.theme === "auto"} onClick={() => patch({ theme: "auto" })}>
            跟随系统
          </Chip>
          <Chip selected={config.theme === "light"} onClick={() => patch({ theme: "light" })}>
            浅色
          </Chip>
          <Chip selected={config.theme === "dark"} onClick={() => patch({ theme: "dark" })}>
            深色
          </Chip>
        </div>
      </div>

      <div className="settings-card">
        <div className="settings-card-title">播放</div>
        <SwitchRow
          title="自动播放下一集"
          description="当前集结束后自动切换"
          checked={config.autoNext}
          onChange={(v) => patch({ autoNext: v })}
        />
        <div className="settings-divider" />
        <div className="switch-row" style={{ cursor: "default" }}>
          <div className="switch-text">
            <div className="switch-title">默认倍速</div>
            <div className="switch-desc">进入播放页时的初始速度</div>
          </div>
          <div className="speed-chips">
            {[0.5, 1, 1.5, 2].map((s) => (
              <Chip
                key={s}
                selected={config.playbackRate === s}
                onClick={() => patch({ playbackRate: s })}
              >
                {s.toFixed(1)}x
              </Chip>
            ))}
          </div>
        </div>
      </div>

      <div className="settings-card">
        <div className="settings-card-title">剧库</div>
        <SwitchRow
          title="随机排序"
          description="每次刷新重新洗牌"
          checked={config.sort === "random"}
          onChange={(v) => patch({ sort: v ? "random" : "default" })}
        />
        <div className="settings-divider" />
        <SwitchRow
          title="下拉刷新时更新服务器剧库"
          description="默认关闭：仅刷新界面快照"
          checked={config.refreshUpdatesServer}
          onChange={(v) => patch({ refreshUpdatesServer: v })}
        />
      </div>

      <div className="settings-card about-card">
        <div className="settings-card-title">关于</div>
        <div className="about-row">
          <div className="switch-title">短剧库 Windows 桌面端</div>
          <div className="version-pill">v{version}</div>
        </div>
        <div className="switch-desc">圆角 · 连续 · 留白 · 开关在右</div>
      </div>
    </div>
  );
}
