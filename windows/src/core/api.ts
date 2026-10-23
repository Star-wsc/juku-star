import { invoke } from "@tauri-apps/api/core";
import type {
  Account,
  DramasResult,
  FollowingItem,
  HlsOpenResult,
  HistoryItem,
  PlaybackOpenResult,
  PlaybackProgress,
  Viewer,
} from "./types";

interface FetchRequest {
  method: string;
  url: string;
  headers?: Record<string, string>;
  body?: string;
}

interface FetchResponse {
  status: number;
  headers: Record<string, string>;
  body: string;
}

type ToastFn = (message: string, kind?: "info" | "error" | "success") => void;

let baseUrl = "";
let viewerId = "";
let toast: ToastFn | null = null;

export function setBaseUrl(url: string) {
  baseUrl = (url || "").replace(/\/+$/, "");
}

export function getBaseUrl() {
  return baseUrl;
}

export function setViewerId(id: string) {
  viewerId = id;
}

export function getViewerId() {
  return viewerId;
}

export function setToast(fn: ToastFn | null) {
  toast = fn;
}

function showToast(message: string, kind: "info" | "error" | "success" = "error") {
  toast?.(message, kind);
}

async function raw(
  method: string,
  path: string,
  body?: unknown,
  opts?: { absolute?: boolean; skipViewer?: boolean }
): Promise<FetchResponse> {
  const url = opts?.absolute ? path : baseUrl + path;
  const headers: Record<string, string> = {
    Accept: "application/json, text/plain, */*",
    "Sec-Fetch-Site": "none",
  };
  if (!opts?.skipViewer && viewerId) headers["X-Juku-Viewer"] = viewerId;
  const req: FetchRequest = {
    method,
    url,
    headers,
  };
  if (body !== undefined) req.body = JSON.stringify(body);
  return invoke<FetchResponse>("http_fetch", { request: req });
}

async function json<T>(
  method: string,
  path: string,
  body?: unknown,
  opts?: { absolute?: boolean; skipViewer?: boolean }
): Promise<T> {
  const resp = await raw(method, path, body, opts);
  const text = resp.body || "{}";
  let parsed: unknown = {};
  try {
    parsed = JSON.parse(text);
  } catch {
    parsed = { error: text };
  }
  if (resp.status >= 400) {
    const err = parsed as { error?: string; message?: string; code?: string };
    const msg = err.error || err.message || `请求失败 (${resp.status})`;
    showToast(msg, "error");
    throw new Error(msg);
  }
  return parsed as T;
}

export async function fetchViewer(confirm = false): Promise<Viewer> {
  const q = confirm ? "?confirm=1" : "";
  return json<Viewer>("GET", `/api/ui/viewer${q}`, undefined, { skipViewer: true });
}

export async function ensureViewer(): Promise<Viewer> {
  const first = await fetchViewer(false);
  if (first?.id) setViewerId(first.id);
  const ready = await fetchViewer(true);
  if (ready?.id) setViewerId(ready.id);
  return ready;
}

export async function login(username: string, password: string): Promise<Account> {
  await ensureViewer();
  const res = await json<{ ok: boolean; account: Account }>(
    "POST",
    "/api/ui/account/login",
    { username, password }
  );
  return res.account;
}

export async function logout(): Promise<void> {
  await json<{ ok: boolean }>("POST", "/api/ui/account/logout", {});
}

export async function fetchDramas(params?: {
  source?: string;
  refresh?: boolean;
  more?: boolean;
}): Promise<DramasResult> {
  const qs = new URLSearchParams();
  if (params?.source && params.source !== "all") qs.set("source", params.source);
  if (params?.refresh) qs.set("refresh", "1");
  if (params?.more) qs.set("more", "1");
  const suffix = qs.toString() ? `?${qs}` : "";
  return json<DramasResult>("GET", `/api/ui/dramas${suffix}`);
}

export async function searchDramas(
  q: string
): Promise<{ data: DramasResult["data"]; total: number }> {
  return json("GET", `/api/ui/search?q=${encodeURIComponent(q)}`);
}

export async function fetchFollowing(): Promise<{ data: FollowingItem[] }> {
  return json("GET", "/api/ui/following");
}

export async function updateFollowing(
  dramaId: string,
  patch: { saved?: boolean; completed?: boolean; acknowledge?: boolean }
): Promise<{ ok: boolean }> {
  return json("POST", "/api/ui/following", { dramaId, ...patch });
}

export async function fetchHistory(): Promise<{ data: HistoryItem[] }> {
  return json("GET", "/api/ui/playback/history");
}

export async function removeHistory(
  dramaId?: string,
  all?: boolean
): Promise<{ ok: boolean }> {
  return json("POST", "/api/ui/playback/history/remove", all ? { all: true } : { dramaId });
}

export async function playbackOpen(
  dramaId: string,
  opts?: { resume?: boolean; fromHistory?: boolean }
): Promise<PlaybackOpenResult> {
  return json("POST", "/api/ui/playback/open", {
    dramaId,
    resume: opts?.resume ?? true,
    fromHistory: opts?.fromHistory ?? false,
  });
}

export async function hlsOpen(
  session: string,
  episode: number,
  opts?: { start?: number | null; quality?: number | null }
): Promise<HlsOpenResult> {
  return json("POST", "/api/ui/playback/hls/open", {
    session,
    episode,
    start: opts?.start ?? undefined,
    quality: opts?.quality ?? undefined,
  });
}

export async function playbackControl(
  session: string,
  action: "heartbeat" | "close",
  progress?: PlaybackProgress
): Promise<{
  state?: string;
  error?: string;
  run?: number;
  duration?: number;
  ok?: boolean;
}> {
  return json("POST", "/api/ui/playback/control", {
    session,
    action,
    progress,
  });
}

export async function playbackProgress(
  session: string,
  progress: PlaybackProgress
): Promise<{ saved: boolean }> {
  return json("POST", "/api/ui/playback/progress", { session, progress });
}

export function resolveMediaUrl(url: string): string {
  if (!url) return "";
  if (/^https?:\/\//i.test(url)) return url;
  if (url.startsWith("/")) return baseUrl + url;
  return baseUrl + "/" + url;
}

/** 与 Android resolveUrl 一致：绝对 URL 原样用；相对路径拼 base */
export function resolveUrl(pathOrUrl: string | undefined | null): string {
  if (!pathOrUrl) return "";
  if (/^https?:\/\//i.test(pathOrUrl)) return pathOrUrl;
  const base = baseUrl.replace(/\/+$/, "");
  return pathOrUrl.startsWith("/") ? base + pathOrUrl : `${base}/${pathOrUrl}`;
}

/** 封面优先原图/原路径，失败再走 /api/ui/image 代理 */
export function coverDirectUrl(cover: string | undefined | null): string {
  return resolveUrl(cover);
}

export function coverProxyUrl(cover: string | undefined | null): string {
  if (!cover) return "";
  const base = baseUrl.replace(/\/+$/, "");
  return `${base}/api/ui/image?url=${encodeURIComponent(cover)}`;
}

export async function loginWithConfig(
  serverUrl: string,
  username: string,
  password: string
): Promise<Account | null> {
  setBaseUrl(serverUrl);
  const viewer = await ensureViewer();
  if (!username) return viewer.account ?? null;
  return login(username, password);
}

export async function pingServer(url: string): Promise<boolean> {
  const prev = baseUrl;
  try {
    setBaseUrl(url);
    await fetchViewer(false);
    return true;
  } catch {
    return false;
  } finally {
    setBaseUrl(prev);
  }
}
