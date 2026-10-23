import { useCallback, useEffect, useMemo, useState } from "react";
import { fetchDramas, playbackOpen, updateFollowing } from "../../core/api";
import type { Drama, Episode } from "../../core/types";
import { useApp } from "../../core/store";
import { nextCoverFallback, useCoverUrl } from "../../core/useCoverUrl";
import { Chip, EmptyState } from "../components";
import { getCachedDramas } from "./LibraryScreen";
import "./Detail.css";

export function DetailScreen() {
  const { detailDramaId, setPage, openPlayer, toast } = useApp();
  const [drama, setDrama] = useState<Drama | null>(
    () => getCachedDramas().find((d) => d.id === detailDramaId) ?? null
  );
  const [loading, setLoading] = useState(!drama);
  const [playing, setPlaying] = useState(false);
  const [following, setFollowing] = useState(false);
  const cover = useCoverUrl(drama?.cover);
  const [coverSrc, setCoverSrc] = useState(cover);

  useEffect(() => {
    setCoverSrc(cover);
  }, [cover]);

  const load = useCallback(async () => {
    if (!detailDramaId) return;
    const hit = getCachedDramas().find((d) => d.id === detailDramaId);
    if (hit) {
      setDrama(hit);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const res = await fetchDramas({});
      const found = (res.data || []).find((d) => d.id === detailDramaId);
      setDrama(found ?? null);
    } catch {
      setDrama(null);
    } finally {
      setLoading(false);
    }
  }, [detailDramaId]);

  useEffect(() => {
    void load();
  }, [load]);

  const episodes = useMemo<Episode[]>(() => {
    if (!drama) return [];
    return Array.from({ length: Math.min(drama.totalEpisode, 12) }).map((_, i) => ({
      vip: drama.vip && i > 2,
      index: i + 1,
      episode: i + 1,
      title: `第${String(i + 1).padStart(2, "0")}集`,
      chapterId: `${drama.id}-${i + 1}`,
      number: i + 1,
      total: drama.totalEpisode,
    }));
  }, [drama]);

  const onPlay = async (ep: number) => {
    if (!drama) return;
    setPlaying(true);
    try {
      await playbackOpen(drama.id, { resume: true });
      openPlayer(drama.id, ep);
    } catch {
      toast("打开播放失败", "error");
    } finally {
      setPlaying(false);
    }
  };

  const onToggleFollow = async () => {
    if (!drama) return;
    try {
      await updateFollowing(drama.id, { saved: !following });
      setFollowing((v) => !v);
      toast(following ? "已取消追剧" : "已加入追剧", "success");
    } catch {
      /* toast */
    }
  };

  const statusText = useMemo(() => {
    if (!drama) return "";
    const map: Record<string, string> = {
      finished: "已完结",
      ongoing: "连载中",
      unknown: "状态未知",
    };
    return map[drama.releaseStatus] || drama.releaseStatus;
  }, [drama]);

  if (loading) {
    return (
      <div className="content-scroll">
        <div className="skeleton" style={{ height: 28, width: 200, marginBottom: 12 }} />
        <div className="skeleton" style={{ height: 320, borderRadius: 16 }} />
      </div>
    );
  }

  if (!drama) {
    return (
      <div className="content-scroll">
        <EmptyState
          title="未找到这部剧"
          description="可能已下架或 ID 失效"
          action={
            <button type="button" className="btn btn-primary" onClick={() => setPage("library")}>
              返回剧库
            </button>
          }
        />
      </div>
    );
  }

  return (
    <div className="content-scroll detail-page">
      <button type="button" className="back-link" onClick={() => setPage("library")}>
        ← 返回
      </button>

      <div className="detail-hero">
        {coverSrc ? (
          <img
            className="detail-cover"
            src={coverSrc}
            alt={drama.title}
            draggable={false}
            onError={(e) => {
              const img = e.currentTarget;
              const tried = (img.dataset.tried || "").split("|").filter(Boolean);
              tried.push(img.src);
              img.dataset.tried = tried.join("|");
              void nextCoverFallback(drama.cover, tried).then((n) => {
                if (n) setCoverSrc(n);
              });
            }}
          />
        ) : (
          <div className="detail-cover detail-cover-fallback">{drama.title.slice(0, 4)}</div>
        )}
        <div className="detail-info">
          <h1 className="t-page-title">{drama.title}</h1>
          <div className="detail-meta">
            <span>{drama.category}</span>
            <span>·</span>
            <span>{statusText}</span>
            <span>·</span>
            <span>{drama.totalEpisode} 集</span>
            <span>·</span>
            <span>{drama.score} 分</span>
            {drama.vip ? <span className="vip-tag">VIP</span> : null}
          </div>
          <p className="detail-desc">{drama.desc}</p>
          <div className="detail-actions">
            <button
              type="button"
              className="btn btn-primary"
              disabled={playing}
              onClick={() => void onPlay(1)}
            >
              ▶ 播放第 1 集
            </button>
            <button type="button" className="btn btn-secondary" onClick={() => void onToggleFollow()}>
              {following ? "已追剧" : "追剧"}
            </button>
          </div>
        </div>
      </div>

      <div className="detail-episodes">
        <div className="t-section">选集</div>
        <div className="chip-row" style={{ marginTop: 14 }}>
          {episodes.map((ep) => (
            <Chip key={ep.index} onClick={() => void onPlay(ep.index)}>
              {ep.title}
            </Chip>
          ))}
        </div>
      </div>
    </div>
  );
}
