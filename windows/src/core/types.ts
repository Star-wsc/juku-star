export interface Viewer {
  ready: boolean;
  id: string;
  account: Account | null;
  sources: string[];
  onlineOnly: boolean;
  requireLogin: boolean;
  allowRegistration: boolean;
}

export interface Account {
  username: string;
  admin: boolean;
  onlineOnly: boolean;
  sources: string[];
  createdAt: string;
}

export interface Drama {
  id: string;
  source: string;
  title: string;
  cover: string;
  desc: string;
  totalEpisode: number;
  category: string;
  releaseStatus: "finished" | "ongoing" | "unknown";
  vip: boolean;
  score: string;
  views: string;
  tags: string[];
  onlineDate: string;
}

export interface Episode {
  vip: boolean;
  index: number;
  episode: number | string;
  title: string;
  taskId?: string;
  danmaku?: boolean;
  chapterId?: string;
  number: number;
  total: number;
}

export interface PlaybackOpenResult {
  session: string;
  dramaId: string;
  title: string;
  releaseStatus: string;
  episodes: Episode[];
  mimeType: string;
  mode: string;
  initialIndex: number;
  initialPosition: number;
  resumePaused: boolean;
  resumeMessage?: string;
}

export interface HlsOpenResult {
  url: string;
  run: number;
  duration: number;
  source: string;
  quality: number;
  qualities: { value: number; label: string }[];
  prefetched: boolean;
}

export interface DramasResult {
  data: Drama[];
  loadedAt: string;
  revision: number;
  sources: Record<string, { status: string; count: number }>;
  hasMore: boolean;
  loading: boolean;
  total: number;
}

export interface HistoryItem {
  dramaId: string;
  source: string;
  title: string;
  chapterId: string;
  episode: number | string;
  index: number;
  total: number;
  position: number;
  duration: number;
  completed: boolean;
  mode: string;
  watchedAt: string;
}

export interface FollowingItem {
  dramaId: string;
  source: string;
  title: string;
  category: string;
  knownEpisodes: number;
  totalEpisode: number;
  newEpisodes: number;
  saved: boolean;
  completed: boolean;
  addedAt: string;
  updatedAt: string;
}

export interface PlaybackProgress {
  run: number;
  sequence: number;
  episode: number | string;
  position: number;
  duration: number;
  completed: boolean;
}

export type PageId =
  | "library"
  | "following"
  | "download"
  | "history"
  | "settings"
  | "detail"
  | "player";
