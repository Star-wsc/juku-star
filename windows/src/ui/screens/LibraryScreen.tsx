import { useCallback, useEffect, useMemo, useState } from "react";
import { fetchDramas, searchDramas } from "../../core/api";
import type { Drama } from "../../core/types";
import {
  isSourceHidden,
  loadConfig,
  saveConfig,
  sourceGroup,
  type AppConfig,
} from "../../core/config";
import { useApp } from "../../core/store";
import { Chip, EmptyState, PosterCard } from "../components";
import "./Library.css";

type SortKey = "random" | "latest" | "score" | "views" | "default";

const SORT_LABEL: Record<SortKey, string> = {
  random: "随机",
  latest: "最新上线",
  score: "评分",
  views: "热度",
  default: "默认",
};

/** 剧库缓存：切页/切筛选不重新拉服务器，只有点「刷新」才拉 */
let dramaCache: Drama[] | null = null;

function shuffle<T>(list: T[]): T[] {
  const arr = [...list];
  for (let i = arr.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [arr[i], arr[j]] = [arr[j], arr[i]];
  }
  return arr;
}

function sortDramas(list: Drama[], sort: SortKey): Drama[] {
  const arr = [...list];
  switch (sort) {
    case "random":
      return shuffle(arr);
    case "latest":
      return arr.sort((a, b) => (b.onlineDate || "").localeCompare(a.onlineDate || ""));
    case "score":
      return arr.sort((a, b) => parseFloat(b.score || "0") - parseFloat(a.score || "0"));
    case "views":
      return arr.sort((a, b) => (b.views || "").localeCompare(a.views || ""));
    default:
      return arr;
  }
}

export function LibraryScreen() {
  const { openDetail, toast } = useApp();
  const [config, setConfig] = useState<AppConfig>(() => loadConfig());
  const [dramas, setDramas] = useState<Drama[]>(() => dramaCache ?? []);
  const [loading, setLoading] = useState(dramaCache === null);
  const [selectedSource, setSelectedSource] = useState(config.selectedSource);
  const [sort, setSort] = useState<SortKey>(config.sort);
  const [hideVip, setHideVip] = useState(config.hideVip);
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [sortOpen, setSortOpen] = useState(false);

  const refreshConfig = useCallback(() => {
    setConfig(loadConfig());
  }, []);

  // 监听设置页改动
  useEffect(() => {
    const onChange = () => refreshConfig();
    window.addEventListener("juku-config-changed", onChange);
    refreshConfig();
    return () => window.removeEventListener("juku-config-changed", onChange);
  }, [refreshConfig]);

  const load = useCallback(async (force = false) => {
    if (!force && dramaCache) {
      setDramas(dramaCache);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const res = await fetchDramas({ refresh: force });
      dramaCache = res.data || [];
      setDramas(dramaCache);
    } catch {
      if (!dramaCache) setDramas([]);
    } finally {
      setLoading(false);
    }
  }, []);

  // 仅首次进剧库拉取；之后靠缓存 + 手动刷新
  useEffect(() => {
    if (dramaCache === null) void load(false);
    else setDramas(dramaCache);
  }, [load]);

  useEffect(() => {
    const next = { ...loadConfig(), sort, hideVip, selectedSource };
    saveConfig(next);
  }, [sort, hideVip, selectedSource]);

  /** 按设置里的双黄蛋开关过滤站源 */
  const sourceVisible = useCallback(
    (d: Drama) => !isSourceHidden(d.source, config),
    [config]
  );

  const visible = useMemo(() => {
    let list = dramas.filter(sourceVisible);
    if (selectedSource && selectedSource !== "all") {
      const g = selectedSource;
      list = list.filter((d) => sourceGroup(d.source) === g || d.source === g);
    }
    if (hideVip) list = list.filter((d) => !d.vip);
    return sortDramas(list, sort);
  }, [dramas, selectedSource, hideVip, sort, sourceVisible]);

  const total = useMemo(() => dramas.filter(sourceVisible).length, [dramas, sourceVisible]);

  /** 筛选 Chip：红果 → 黄果 → 黄豆 → 全部（最后） */
  const chips = useMemo(() => {
    const list: { id: string; label: string }[] = [{ id: "hongguo", label: "红果" }];
    if (config.showHuangguo) list.push({ id: "huangguo", label: "黄果" });
    if (config.showHuangdou) list.push({ id: "huangdou", label: "黄豆" });
    list.push({ id: "all", label: "全部" });
    return list;
  }, [config.showHuangguo, config.showHuangdou]);

  // 若当前选中源被设置关掉，回落到红果
  useEffect(() => {
    if (!chips.some((c) => c.id === selectedSource)) {
      setSelectedSource("hongguo");
    }
  }, [chips, selectedSource]);

  const onSearch = async () => {
    const q = query.trim();
    if (!q) {
      await load(true);
      return;
    }
    setSearching(true);
    try {
      const res = await searchDramas(q);
      dramaCache = res.data || [];
      setDramas(dramaCache);
    } catch {
      /* toast shown */
    } finally {
      setSearching(false);
    }
  };

  return (
    <div className="content-scroll">
      <div className="library-header">
        <div>
          <h1 className="t-page-title">短剧库</h1>
          <p className="t-page-sub">发现更多好看的故事 · 共 {total.toLocaleString()} 部</p>
        </div>
        <div className="search-pill">
          <input
            value={query}
            placeholder="搜索剧名…"
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void onSearch();
            }}
          />
          <button type="button" className="search-btn" onClick={() => void onSearch()} aria-label="搜索">
            ⌕
          </button>
        </div>
      </div>

      <div className="chip-row library-chips">
        {chips.map((c) => (
          <Chip
            key={c.id}
            selected={selectedSource === c.id}
            onClick={() => setSelectedSource(c.id)}
          >
            {c.label}
          </Chip>
        ))}
        <div className="sort-wrap">
          <Chip selected={sortOpen} onClick={() => setSortOpen((v) => !v)}>
            排序: {SORT_LABEL[sort]}
          </Chip>
          {sortOpen ? (
            <div className="sort-menu">
              {(Object.keys(SORT_LABEL) as SortKey[]).map((k) => (
                <button
                  key={k}
                  type="button"
                  className={`sort-item ${sort === k ? "active" : ""}`}
                  onClick={() => {
                    setSort(k);
                    setSortOpen(false);
                    if (k === "random") toast("已重新洗牌", "success");
                  }}
                >
                  {SORT_LABEL[k]}
                </button>
              ))}
            </div>
          ) : null}
        </div>
        <Chip selected={hideVip} onClick={() => setHideVip((v) => !v)}>
          隐藏VIP
        </Chip>
        <button
          type="button"
          className="btn btn-secondary refresh-btn"
          onClick={() => {
            void load(true);
            if (sort === "random") toast("已刷新并重新洗牌", "success");
            else toast("已刷新剧库快照", "success");
          }}
          disabled={loading || searching}
        >
          刷新
        </button>
      </div>

      {loading ? (
        <div className="poster-grid">
          {Array.from({ length: 10 }).map((_, i) => (
            <div key={i} className="skeleton" style={{ width: 168, height: 224, borderRadius: 14 }} />
          ))}
        </div>
      ) : visible.length === 0 ? (
        <EmptyState
          title="暂无剧集"
          description="可在设置中打开黄果/黄豆，或点刷新重新拉取"
          action={
            <button type="button" className="btn btn-primary" onClick={() => void load(true)}>
              刷新剧库
            </button>
          }
        />
      ) : (
        <div className="poster-grid">
          {visible.map((d) => (
            <PosterCard
              key={d.id}
              title={d.title}
              subtitle={`${d.totalEpisode}集 · ${d.score || "-"}`}
              cover={d.cover}
              badge={d.vip ? "VIP" : undefined}
              onClick={() => openDetail(d.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

/** 供其它模块读取缓存（详情页） */
export function getCachedDramas(): Drama[] {
  return dramaCache ?? [];
}

export function invalidateDramaCache() {
  dramaCache = null;
}
