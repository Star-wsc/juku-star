import React, { createContext, useCallback, useContext, useMemo, useState } from "react";

export interface ToastItem {
  id: number;
  message: string;
  kind: "info" | "error" | "success";
}

interface AppState {
  phase: "login" | "app";
  setPhase: (p: "login" | "app") => void;
  accountName: string | null;
  setAccountName: (n: string | null) => void;
  page: string;
  setPage: (p: string) => void;
  detailDramaId: string | null;
  openDetail: (id: string) => void;
  playerDramaId: string | null;
  openPlayer: (id: string, episode?: number) => void;
  playerEpisode: number;
  toasts: ToastItem[];
  toast: (message: string, kind?: "info" | "error" | "success") => void;
  serverUrl: string;
  setServerUrl: (url: string) => void;
}

const Ctx = createContext<AppState | null>(null);

let toastSeq = 1;

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [phase, setPhase] = useState<"login" | "app">("login");
  const [accountName, setAccountName] = useState<string | null>(null);
  const [page, setPage] = useState("library");
  const [detailDramaId, setDetailDramaId] = useState<string | null>(null);
  const [playerDramaId, setPlayerDramaId] = useState<string | null>(null);
  const [playerEpisode, setPlayerEpisode] = useState(1);
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const [serverUrl, setServerUrl] = useState("");

  const toast = useCallback((message: string, kind: "info" | "error" | "success" = "info") => {
    const id = toastSeq++;
    setToasts((list) => [...list, { id, message, kind }]);
    window.setTimeout(() => {
      setToasts((list) => list.filter((t) => t.id !== id));
    }, 2800);
  }, []);

  const openDetail = useCallback((id: string) => {
    setDetailDramaId(id);
    setPage("detail");
  }, []);

  const openPlayer = useCallback((id: string, episode = 1) => {
    setPlayerDramaId(id);
    setPlayerEpisode(episode);
    setPage("player");
  }, []);

  const value = useMemo(
    () => ({
      phase,
      setPhase,
      accountName,
      setAccountName,
      page,
      setPage,
      detailDramaId,
      openDetail,
      playerDramaId,
      openPlayer,
      playerEpisode,
      toasts,
      toast,
      serverUrl,
      setServerUrl,
    }),
    [
      phase,
      accountName,
      page,
      detailDramaId,
      openDetail,
      playerDramaId,
      openPlayer,
      playerEpisode,
      toasts,
      toast,
      serverUrl,
    ]
  );

  return (
    <Ctx.Provider value={value}>
      {children}
      <div className="toast-wrap">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.kind === "info" ? "" : t.kind}`}>
            {t.message}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}

export function useApp() {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useApp outside provider");
  return ctx;
}
