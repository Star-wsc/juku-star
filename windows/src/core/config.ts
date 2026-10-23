const STORE_KEY = "juku-desktop-config";

export interface AppConfig {
  serverUrl: string;
  username: string;
  password: string;
  theme: "auto" | "light" | "dark";
  sort: "random" | "latest" | "score" | "views" | "default";
  hideVip: boolean;
  autoNext: boolean;
  playbackRate: number;
  refreshUpdatesServer: boolean;
  showHuangguo: boolean;
  showHuangdou: boolean;
  selectedSource: string;
}

export const defaultConfig: AppConfig = {
  serverUrl: "",
  username: "",
  password: "",
  theme: "light",
  sort: "random",
  hideVip: false,
  autoNext: true,
  playbackRate: 1,
  refreshUpdatesServer: false,
  showHuangguo: false,
  showHuangdou: false,
  selectedSource: "hongguo",
};

export function loadConfig(): AppConfig {
  try {
    const raw = localStorage.getItem(STORE_KEY);
    if (!raw) return { ...defaultConfig };
    return { ...defaultConfig, ...JSON.parse(raw) };
  } catch {
    return { ...defaultConfig };
  }
}

export function saveConfig(config: AppConfig) {
  localStorage.setItem(STORE_KEY, JSON.stringify(config));
}

export function patchConfig(patch: Partial<AppConfig>): AppConfig {
  const next = { ...loadConfig(), ...patch };
  saveConfig(next);
  return next;
}

export function normalizeServerUrl(url: string): string {
  let u = (url || "").trim();
  if (!u) return "";
  if (!/^https?:\/\//i.test(u)) u = "http://" + u;
  return u.replace(/\/+$/, "");
}

/** 是否已配置服务器与账号（进入主界面的前提） */
export function hasCredentials(): boolean {
  const c = loadConfig();
  return Boolean(c.serverUrl && c.username && c.password);
}

export function sourceGroup(source: string | null | undefined): string {
  const s = (source || "").toLowerCase();
  if (!s) return "other";
  if (s.startsWith("hongguo")) return "hongguo";
  if (s.startsWith("huangguo") || s === "cloudfront") return "huangguo";
  if (s.startsWith("huangdou")) return "huangdou";
  return s;
}

export const SOURCE_LABEL: Record<string, string> = {
  hongguo: "红果",
  huangguo: "黄果",
  huangdou: "黄豆",
};

export function isSourceHidden(
  source: string | null | undefined,
  cfg: Pick<AppConfig, "showHuangguo" | "showHuangdou">
): boolean {
  const g = sourceGroup(source);
  if (g === "huangguo") return !cfg.showHuangguo;
  if (g === "huangdou") return !cfg.showHuangdou;
  return false;
}
