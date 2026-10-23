import { useCallback, useEffect, useRef, useState } from "react";
import Hls from "hls.js";
import {
  hlsOpen,
  playbackControl,
  playbackOpen,
  playbackProgress,
  resolveUrl,
} from "../../core/api";
import type { Episode } from "../../core/types";
import { useApp } from "../../core/store";
import { EmptyState } from "../components";
import "./Player.css";

function isHlsMedia(url: string): boolean {
  const path = url.split("?")[0].split("#")[0].toLowerCase();
  if (path.endsWith(".m3u8") || path.endsWith(".m3u")) return true;
  if (path.endsWith(".mp4") || path.endsWith(".m4v") || path.endsWith(".mov")) return false;
  if (url.includes("/playback/hls/")) return true;
  return true;
}

export function PlayerScreen() {
  const { playerDramaId, playerEpisode, setPage, openPlayer, toast } = useApp();
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const sessionRef = useRef<string | null>(null);
  const runRef = useRef(0);
  const seqRef = useRef(0);
  const episodeRef = useRef(playerEpisode);
  const heartbeatRef = useRef<number | null>(null);
  const progressRef = useRef<number | null>(null);
  const episodesRef = useRef<Episode[]>([]);

  const [episode, setEpisode] = useState(playerEpisode);
  const [title, setTitle] = useState("");
  const [episodeTitle, setEpisodeTitle] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [playing, setPlaying] = useState(false);
  const [progress, setProgress] = useState(0);

  const clearTimers = useCallback(() => {
    if (heartbeatRef.current) window.clearInterval(heartbeatRef.current);
    if (progressRef.current) window.clearInterval(progressRef.current);
    heartbeatRef.current = null;
    progressRef.current = null;
  }, []);

  const destroyHls = useCallback(() => {
    hlsRef.current?.destroy();
    hlsRef.current = null;
  }, []);

  const reportProgress = useCallback(async (completed = false) => {
    const video = videoRef.current;
    const session = sessionRef.current;
    if (!session || !video) return;
    seqRef.current += 1;
    try {
      await playbackProgress(session, {
        run: runRef.current,
        sequence: seqRef.current,
        episode: episodeRef.current,
        position: video.currentTime || 0,
        duration: video.duration || 0,
        completed,
      });
    } catch {
      /* silent */
    }
  }, []);

  const startTimers = useCallback(() => {
    const video = videoRef.current;
    const session = sessionRef.current;
    if (!video || !session) return;
    clearTimers();
    heartbeatRef.current = window.setInterval(() => {
      void playbackControl(session, "heartbeat");
    }, 60_000);
    progressRef.current = window.setInterval(() => {
      if (!video.paused) void reportProgress(false);
    }, 5_000);
  }, [clearTimers, reportProgress]);

  const playEpisode = useCallback(
    async (ep: number, startPos = 0) => {
      if (!playerDramaId) return;
      setLoading(true);
      setError(null);
      clearTimers();
      destroyHls();

      const video = videoRef.current;
      if (video) {
        video.onplay = null;
        video.onpause = null;
        video.onended = null;
        video.ontimeupdate = null;
        video.pause();
        video.removeAttribute("src");
        video.load();
      }

      try {
        if (!sessionRef.current) {
          const open = await playbackOpen(playerDramaId, { resume: true });
          sessionRef.current = open.session;
          setTitle(open.title || playerDramaId);
          episodesRef.current = open.episodes || [];
          if (open.resumeMessage) toast(open.resumeMessage, "success");
        }
        const session = sessionRef.current;
        const meta = episodesRef.current.find(
          (e) => Number(e.episode) === ep || e.index === ep
        );
        setEpisodeTitle(meta?.title || `第${ep}集`);
        episodeRef.current = ep;
        setEpisode(ep);

        const opened = await hlsOpen(session, ep, {
          start: startPos > 0 ? startPos : undefined,
        });
        runRef.current = Number(opened.run) || 0;

        const mediaUrl = resolveUrl(opened.url);
        const el = videoRef.current;
        if (!el) throw new Error("播放器未就绪");

        const useNative = !isHlsMedia(mediaUrl) || !!el.canPlayType("application/vnd.apple.mpegurl");
        if (useNative) {
          destroyHls();
          el.src = mediaUrl;
        } else if (Hls.isSupported()) {
          destroyHls();
          const inst = new Hls({ enableWorker: true });
          hlsRef.current = inst;
          inst.loadSource(mediaUrl);
          inst.attachMedia(el);
          inst.on(Hls.Events.ERROR, (_e, data) => {
            if (data?.fatal) {
              setError("视频加载失败，请检查网络或稍后重试");
              setLoading(false);
            }
          });
        } else {
          el.src = mediaUrl;
        }

        el.onplay = () => {
          setPlaying(true);
          startTimers();
        };
        el.onpause = () => {
          setPlaying(false);
          void reportProgress(false);
        };
        el.onended = () => {
          void reportProgress(true);
          setPlaying(false);
          if (loadConfigAutoNext()) {
            const next = ep + 1;
            const has = episodesRef.current.some((e) => Number(e.episode) === next);
            if (has) {
              openPlayer(playerDramaId, next);
              return;
            }
          }
          toast("本集播放完成", "success");
        };
        el.ontimeupdate = () => {
          if (el.duration) setProgress((el.currentTime / el.duration) * 100);
        };

        await new Promise<void>((resolve) => {
          const onReady = () => {
            el.removeEventListener("loadedmetadata", onReady);
            if (startPos > 0) el.currentTime = startPos;
            resolve();
          };
          if (el.readyState >= 1) {
            if (startPos > 0) el.currentTime = startPos;
            resolve();
          } else {
            el.addEventListener("loadedmetadata", onReady);
          }
        });

        await el.play().catch(() => undefined);
      } catch (e) {
        setError(e instanceof Error ? e.message : "播放失败");
      } finally {
        setLoading(false);
      }
    },
    [playerDramaId, openPlayer, startTimers, reportProgress, toast, clearTimers, destroyHls]
  );

  useEffect(() => {
    episodeRef.current = playerEpisode;
    void playEpisode(playerEpisode);
    return () => {
      clearTimers();
      destroyHls();
      const s = sessionRef.current;
      sessionRef.current = null;
      if (s) void playbackControl(s, "close");
    };
  }, [playerDramaId, playerEpisode, playEpisode, clearTimers, destroyHls]);

  const onSeek = (e: React.MouseEvent<HTMLDivElement>) => {
    const video = videoRef.current;
    if (!video || !video.duration) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
    video.currentTime = ratio * video.duration;
    setProgress(ratio * 100);
  };

  const togglePlay = () => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) void video.play();
    else video.pause();
  };

  if (!playerDramaId) {
    return (
      <div className="content-scroll">
        <EmptyState
          title="未选择剧集"
          action={
            <button type="button" className="btn btn-primary" onClick={() => setPage("library")}>
              去剧库
            </button>
          }
        />
      </div>
    );
  }

  return (
    <div className="player-page">
      <div className="player-top">
        <button type="button" className="back-link" onClick={() => setPage("detail")}>
          ← 返回详情
        </button>
        <div className="player-titles">
          <div className="player-drama-title">{title || playerDramaId}</div>
          <div className="player-ep-title">{episodeTitle}</div>
        </div>
      </div>

      <div className="player-stage">
        <video ref={videoRef} className="player-video" controls={false} playsInline />
        {loading ? <div className="player-loading">加载中…</div> : null}
        {error ? (
          <div className="player-error">
            <EmptyState
              icon="!"
              title="播放失败"
              description={error}
              action={
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={() => void playEpisode(episode)}
                >
                  重试
                </button>
              }
            />
          </div>
        ) : null}
      </div>

      <div className="player-controls">
        <button type="button" className="btn btn-primary play-toggle" onClick={togglePlay}>
          {playing ? "⏸" : "▶"}
        </button>
        <div className="progress-bar" onClick={onSeek} role="presentation">
          <div className="progress-fill" style={{ width: `${progress}%` }} />
          <div className="progress-knob" style={{ left: `${progress}%` }} />
        </div>
        <div className="player-actions">
          <button
            type="button"
            className="btn btn-secondary"
            disabled={episode <= 1}
            onClick={() => openPlayer(playerDramaId, Math.max(1, episode - 1))}
          >
            上一集
          </button>
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => openPlayer(playerDramaId, episode + 1)}
          >
            下一集
          </button>
        </div>
      </div>
    </div>
  );
}

function loadConfigAutoNext(): boolean {
  try {
    const raw = localStorage.getItem("juku-desktop-config");
    if (!raw) return true;
    const cfg = JSON.parse(raw) as { autoNext?: boolean };
    return cfg.autoNext !== false;
  } catch {
    return true;
  }
}
