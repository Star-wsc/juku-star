import { useEffect, useState } from "react";
import { fetchFollowing, fetchHistory, removeHistory } from "../../core/api";
import type { FollowingItem, HistoryItem } from "../../core/types";
import { useApp } from "../../core/store";
import { useCoverUrl } from "../../core/useCoverUrl";
import { EmptyState } from "../components";

export function FollowingScreen() {
  const { openDetail, setPage, toast } = useApp();
  const [list, setList] = useState<FollowingItem[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    void fetchFollowing()
      .then((res) => setList(res.data || []))
      .catch(() => setList([]))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="content-scroll">
      <h1 className="t-page-title">追剧</h1>
      <p className="t-page-sub">收藏的剧集 · 共 {list.length} 部</p>
      <div style={{ height: 20 }} />
      {loading ? (
        <div className="skeleton" style={{ height: 120, borderRadius: 16 }} />
      ) : list.length === 0 ? (
        <EmptyState
          title="还没有追剧"
          description="在详情页点击「追剧」即可加入"
          action={
            <button type="button" className="btn btn-primary" onClick={() => setPage("library")}>
              去剧库看看
            </button>
          }
        />
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {list.map((item) => (
            <div
              key={item.dramaId}
              className="list-row"
              onClick={() => {
                if (item.dramaId) openDetail(item.dramaId);
                else toast("无法打开", "error");
              }}
            >
              <div className="list-body">
                <div className="list-title">{item.title}</div>
                <div className="list-sub">
                  {item.category} · {item.knownEpisodes}/{item.totalEpisode} 集
                  {item.newEpisodes > 0 ? ` · ${item.newEpisodes} 集更新` : ""}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function HistoryScreen() {
  const { openPlayer, openDetail, toast } = useApp();
  const [list, setList] = useState<HistoryItem[]>([]);
  const [loading, setLoading] = useState(true);

  const load = async () => {
    setLoading(true);
    try {
      const res = await fetchHistory();
      setList(res.data || []);
    } catch {
      setList([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  return (
    <div className="content-scroll">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
        <div>
          <h1 className="t-page-title">历史</h1>
          <p className="t-page-sub">最近观看 · 共 {list.length} 条</p>
        </div>
        {list.length > 0 ? (
          <button
            type="button"
            className="btn btn-secondary"
            onClick={async () => {
              await removeHistory(undefined, true);
              toast("已清空历史", "success");
              await load();
            }}
          >
            清空
          </button>
        ) : null}
      </div>
      <div style={{ height: 20 }} />
      {loading ? (
        <div className="skeleton" style={{ height: 120, borderRadius: 16 }} />
      ) : list.length === 0 ? (
        <EmptyState title="暂无观看记录" description="看过的剧会出现在这里" />
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {list.map((item) => (
            <div
              key={`${item.dramaId}-${item.watchedAt}`}
              className="list-row"
              onClick={() => openPlayer(item.dramaId, item.index || Number(item.episode) || 1)}
            >
              <div className="list-body">
                <div className="list-title">{item.title}</div>
                <div className="list-sub">
                  第 {item.index || item.episode} 集 / 共 {item.total} 集
                  {item.completed ? " · 已看完" : ` · 看到 ${Math.round(item.position)}s`}
                </div>
              </div>
              <button
                type="button"
                className="btn btn-ghost"
                onClick={(e) => {
                  e.stopPropagation();
                  openDetail(item.dramaId);
                }}
              >
                详情
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function DownloadScreen() {
  return (
    <div className="content-scroll">
      <h1 className="t-page-title">下载</h1>
      <p className="t-page-sub">服务端下载任务管理</p>
      <div style={{ height: 20 }} />
      <EmptyState
        title="下载管理"
        description="服务端下载与合并任务将在此展示"
        action={
          <button type="button" className="btn btn-secondary" disabled>
            敬请期待
          </button>
        }
      />
    </div>
  );
}

export function HistoryThumb({ cover }: { cover: string }) {
  const src = useCoverUrl(cover);
  return <img className="list-thumb" src={src} alt="" />;
}
