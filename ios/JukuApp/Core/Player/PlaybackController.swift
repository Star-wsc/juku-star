import Foundation
import AVKit
import Combine

/// 播放会话控制器 — 对齐 iOS 文档 §6.3
/// open → hls/open → 播放 → heartbeat / progress(sequence) → prefetch → close
@MainActor
final class PlaybackController: ObservableObject {
    @Published var session: String?
    @Published var run: Int = 0
    @Published var episodes: [Episode] = []
    @Published var currentEpisode = 1
    @Published var isPlaying = false
    @Published var position: Double = 0
    @Published var duration: Double = 0
    @Published var speed: Double = Prefs.defaultSpeed
    @Published var qualityLabel = Prefs.defaultQuality
    @Published var fromHistory = false
    @Published var offlineMode = false
    @Published var statusText = "就绪"
    @Published var error: String?
    @Published var player: AVPlayer?
    @Published var showControls = true

    private var sequence = 0
    private var heartbeatTask: Task<Void, Never>?
    private var progressTask: Task<Void, Never>?
    private var tickTask: Task<Void, Never>?
    private var uiHideTask: Task<Void, Never>?
    private var api: APIClient?

    // MARK: - Online (HLS)

    func open(api: APIClient, dramaId: String, episode: Int, fromHistory: Bool, position: Double = 0) async {
        self.api = api
        self.fromHistory = fromHistory
        self.offlineMode = false
        self.currentEpisode = episode
        self.sequence = 0
        self.error = nil

        await close(keepPlayer: false)

        do {
            var body: [String: Any] = ["dramaId": dramaId, "resume": true]
            // fromHistory=true 仅历史续播入口
            body["fromHistory"] = fromHistory
            let open = try await api.post(PlaybackOpenResponse.self, path: Endpoints.playbackOpen, body: body)
            session = open.session
            episodes = open.episodes ?? []
            if position > 0 {
                self.position = position
            } else if let p = open.initialPosition {
                self.position = p
            }
            if let d = open.initialIndex, position <= 0 {
                currentEpisode = d
            }
            try await startHls(episode: currentEpisode)
            startTimers()
            statusText = "播放中 · run=\(run) · seq↑ · hb 70s"
        } catch {
            let msg = (error as? ApiErrorBody)?.error
                ?? (error as? JukuAPIError)?.errorDescription
                ?? error.localizedDescription
            self.error = msg
            statusText = "打开失败"
        }
    }

    private func startHls(episode: Int) async throws {
        guard let api, let session else { throw JukuAPIError("无会话") }
        let res = try await api.post(HlsOpenResponse.self, path: Endpoints.hlsOpen, body: [
            "session": session,
            "episode": episode,
            "start": position,
            "quality": 0,
        ])
        // run 必须是 JSON 整数（Android 踩过坑）
        run = res.run ?? 0
        if let d = res.duration, d > 0 { duration = d }
        if let urlStr = res.url, let base = api.baseURL {
            let full: URL
            if urlStr.hasPrefix("http") {
                full = URL(string: urlStr) ?? base
            } else {
                full = URL(string: urlStr, relativeTo: base) ?? base
            }
            attach(playerURL: full)
        }
    }

    // MARK: - Offline (本地合并 TS)

    func openOffline(url: URL, episode: Int, title: String) async {
        await close(keepPlayer: false)
        offlineMode = true
        currentEpisode = episode
        sequence = 0
        episodes = [Episode(vip: false, index: episode, episode: episode, title: title,
                            taskId: nil, chapterId: nil, number: episode, total: episode)]
        attach(playerURL: url)
        statusText = "离线播放 · \(title)"
    }

    private func attach(playerURL: URL) {
        let item = AVPlayerItem(url: playerURL)
        if let player {
            player.replaceCurrentItem(with: item)
        } else {
            player = AVPlayer(playerItem: item)
        }
        player?.automaticallyWaitsToMinimizeStalling = true
        player?.rate = Float(speed)
        if position > 0 {
            player?.seek(to: CMTime(seconds: position, preferredTimescale: 600))
        }
        player?.play()
        isPlaying = true
    }

    func switchEpisode(_ ep: Int) async {
        guard !offlineMode, let api, let session else { return }
        guard ep >= 1 else { return }
        currentEpisode = ep
        position = 0
        sequence = 0
        fromHistory = false
        do {
            try await startHls(episode: ep)
            // 切集产生新 run，progress 必须匹配
            statusText = "第\(ep)集 · run=\(run)"
        } catch {
            self.error = error.localizedDescription
        }
    }

    func togglePlay() {
        guard let player else { return }
        if isPlaying {
            player.pause()
            isPlaying = false
        } else {
            player.play()
            player.rate = Float(speed)
            isPlaying = true
        }
        showControlsTemporarily()
    }

    func setSpeed(_ s: Double) {
        speed = s
        Prefs.defaultSpeed = s
        if isPlaying { player?.rate = Float(s) }
    }

    func seek(to seconds: Double) {
        position = max(0, seconds)
        player?.seek(to: CMTime(seconds: position, preferredTimescale: 600))
        showControlsTemporarily()
    }

    // MARK: - Timers: heartbeat 70s / progress 10s / UI tick

    private func startTimers() {
        heartbeatTask?.cancel()
        progressTask?.cancel()
        tickTask?.cancel()

        if !offlineMode {
            heartbeatTask = Task {
                while !Task.isCancelled {
                    try? await Task.sleep(nanoseconds: 70_000_000_000)
                    await self.heartbeat()
                }
            }
            progressTask = Task {
                while !Task.isCancelled {
                    try? await Task.sleep(nanoseconds: 10_000_000_000)
                    await self.reportProgress()
                }
            }
        }
        tickTask = Task {
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 400_000_000)
                if let t = self.player?.currentItem?.currentTime().seconds, t.isFinite {
                    self.position = t
                }
                if let d = self.player?.currentItem?.duration.seconds, d.isFinite, d > 0 {
                    self.duration = d
                }
                // 自动下一集
                if self.isPlaying, self.duration > 0, self.position >= self.duration - 0.2 {
                    if Prefs.autoNext, !self.offlineMode {
                        await self.switchEpisode(self.currentEpisode + 1)
                    } else {
                        self.isPlaying = false
                    }
                }
            }
        }
    }

    func heartbeat() async {
        guard let api, let session else { return }
        var body: [String: Any] = ["session": session, "action": "heartbeat"]
        if sequence > 0 {
            body["progress"] = progressDict()
        }
        do {
            _ = try await api.post(HeartbeatResponse.self, path: Endpoints.playbackControl, body: body)
            statusText = "心跳 OK · seq=\(sequence)"
        } catch {
            statusText = "心跳失败"
        }
    }

    func reportProgress() async {
        guard let api, let session else { return }
        sequence += 1 // 必须单调递增
        do {
            _ = try await api.post(Bool.self, path: Endpoints.playbackProgress, body: [
                "session": session,
                "progress": progressDict(),
            ])
        } catch {
            // 400 多为 run 不匹配 — 保留 status 提示
            statusText = "进度上报失败"
        }
        // 剩 90s 预缓存下一集
        if Prefs.prefetch, duration > 0, duration - position < 90, duration - position > 0 {
            _ = try? await api.post(Bool.self, path: Endpoints.playbackPrefetch, body: [
                "session": session,
                "episode": currentEpisode + 1,
                "run": run,
                "version": 0,
            ])
            statusText = "预缓存…"
        }
    }

    private func progressDict() -> [String: Any] {
        [
            "run": run,
            "sequence": sequence,
            "episode": currentEpisode,
            "position": position,
            "duration": duration,
            "completed": duration > 0 && position / duration > 0.95,
        ]
    }

    func close(keepPlayer: Bool = false) async {
        heartbeatTask?.cancel()
        progressTask?.cancel()
        tickTask?.cancel()
        uiHideTask?.cancel()

        if !offlineMode, let api, let session {
            var body: [String: Any] = ["session": session, "action": "close"]
            body["progress"] = progressDict()
            _ = try? await api.post(Bool.self, path: Endpoints.playbackControl, body: body)
        }
        if !keepPlayer {
            player?.pause()
            player = nil
            isPlaying = false
        }
        session = nil
    }

    func showControlsTemporarily() {
        showControls = true
        uiHideTask?.cancel()
        uiHideTask = Task {
            try? await Task.sleep(nanoseconds: 3_200_000_000)
            if self.isPlaying { self.showControls = false }
        }
    }
}
