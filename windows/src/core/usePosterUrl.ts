import { useEffect, useState } from "react";
import { invoke } from "@tauri-apps/api/core";

const cache = new Map<string, string>();

export function posterAbsoluteUrl(cover: string | undefined, baseUrl: string): string {
  if (!cover) return "";
  const base = baseUrl.replace(/\/+$/, "");
  if (/^https?:\/\//i.test(cover)) {
    return `${base}/api/ui/image?url=${encodeURIComponent(cover)}`;
  }
  if (cover.startsWith("/")) return base + cover;
  return `${base}/api/ui/image?url=${encodeURIComponent(cover)}`;
}

/** 直连优先；失败则走 Rust 代理转 data URL */
export function usePosterUrl(cover: string | undefined, baseUrl: string): string {
  const [src, setSrc] = useState<string>(() => (cover ? posterAbsoluteUrl(cover, baseUrl) : ""));

  useEffect(() => {
    if (!cover) {
      setSrc("");
      return;
    }
    const direct = posterAbsoluteUrl(cover, baseUrl);
    const key = `${baseUrl}|${cover}`;
    const hit = cache.get(key);
    if (hit && hit.startsWith("data:")) {
      setSrc(hit);
      return;
    }
    setSrc(direct);
    return;
  }, [cover, baseUrl]);

  return src;
}

/** 图片加载失败时兜底走代理 */
export async function loadPosterFallback(cover: string, baseUrl: string): Promise<string> {
  const key = `${baseUrl}|${cover}`;
  const hit = cache.get(key);
  if (hit) return hit;
  const target = posterAbsoluteUrl(cover, baseUrl);
  try {
    const b64 = await invoke<string>("fetch_image_base64", { url: target });
    const mime = cover.toLowerCase().includes(".png")
      ? "image/png"
      : cover.toLowerCase().includes(".webp")
        ? "image/webp"
        : "image/jpeg";
    const dataUrl = `data:${mime};base64,${b64}`;
    cache.set(key, dataUrl);
    return dataUrl;
  } catch {
    return "";
  }
}
