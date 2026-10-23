import { useEffect, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { coverDirectUrl, coverProxyUrl, getBaseUrl } from "./api";

const cache = new Map<string, string>();

/**
 * 封面地址解析（对齐 Android resolveUrl）：
 * 1. 绝对 URL → 原样直连（黄果/黄豆 CDN 图）
 * 2. 相对 /api/ui/image?... → base + path
 * 3. 失败 → /api/ui/image?url= 代理
 * 4. 仍失败 → Rust 代理转 data URL
 */
export function coverCandidates(cover: string | undefined | null): string[] {
  if (!cover) return [];
  const direct = coverDirectUrl(cover);
  const proxy = coverProxyUrl(cover);
  const list = [direct];
  if (proxy && proxy !== direct) list.push(proxy);
  return list;
}

export function useCoverUrl(cover: string | undefined | null): string {
  const [src, setSrc] = useState(() => coverCandidates(cover)[0] || "");

  useEffect(() => {
    const key = cover || "";
    const hit = key ? cache.get(key) : "";
    if (hit) {
      setSrc(hit);
      return;
    }
    const first = coverCandidates(cover)[0] || "";
    setSrc(first);
  }, [cover]);

  return src;
}

/** img onError 逐级回退 */
export async function nextCoverFallback(
  cover: string | undefined | null,
  tried: string[]
): Promise<string | null> {
  if (!cover) return null;
  const cands = coverCandidates(cover);
  for (const c of cands) {
    if (!tried.includes(c)) return c;
  }
  // Rust 代理
  const key = cover;
  const hit = cache.get(key);
  if (hit && hit.startsWith("data:")) return hit;
  try {
    const target = coverProxyUrl(cover) || coverDirectUrl(cover);
    const b64 = await invoke<string>("fetch_image_base64", { url: target });
    const lower = cover.toLowerCase();
    const mime = lower.includes(".png")
      ? "image/png"
      : lower.includes(".webp")
        ? "image/webp"
        : lower.includes(".gif")
          ? "image/gif"
          : "image/jpeg";
    const dataUrl = `data:${mime};base64,${b64}`;
    cache.set(key, dataUrl);
    return dataUrl;
  } catch {
    return null;
  }
}

export function primeCoverCache(cover: string, src: string) {
  if (cover && src.startsWith("data:")) cache.set(cover, src);
}

export { getBaseUrl };
