import Foundation

/// HLS 分片下载合并 — 对应 Android HlsDownloader
/// 串行下载，防止打满 4 个播放会话
actor DownloadManager {
    static let shared = DownloadManager()

    struct TaskItem: Identifiable, Equatable {
        let id: String
        let dramaId: String
        let episode: Int
        var title: String
        var status: Status
        var progress: Double
        var totalBytes: Int64
        var downloadedBytes: Int64
        var error: String?

        enum Status: String {
            case queued, parsing, running, success, failed, canceled, paused
        }
    }

    private(set) var tasks: [TaskItem] = []
    private var queue: [(dramaId: String, episode: Int)] = []
    private var running = false

    func enqueue(dramaId: String, episode: Int, api: APIClient) async {
        // APIClient.baseURL 为 nonisolated(unsafe)，此处同步读取
        let id = "\(dramaId)#\(episode)"
        if let i = tasks.firstIndex(where: { $0.id == id }), tasks[i].status == .success {
            return
        }
        if let i = tasks.firstIndex(where: { $0.id == id }) {
            tasks[i].status = .queued
        } else {
            tasks.append(TaskItem(
                id: id, dramaId: dramaId, episode: episode,
                title: String(format: "第%02d集", episode),
                status: .queued, progress: 0, totalBytes: 0, downloadedBytes: 0
            ))
        }
        queue.append((dramaId, episode))
        await pump(api: api)
    }

    func pause(id: String) {
        if let i = tasks.firstIndex(where: { $0.id == id }) {
            tasks[i].status = .paused
        }
    }

    func resume(id: String, api: APIClient) async {
        if let i = tasks.firstIndex(where: { $0.id == id }) {
            tasks[i].status = .queued
            queue.append((tasks[i].dramaId, tasks[i].episode))
        }
        await pump(api: api)
    }

    func remove(id: String) {
        tasks.removeAll { $0.id == id }
        queue.removeAll { "\($0.dramaId)#\($0.episode)" == id }
    }

    func clearFinished() {
        tasks.removeAll { $0.status == .success || $0.status == .failed || $0.status == .canceled }
    }

    nonisolated func localURL(dramaId: String, episode: Int) -> URL {
        let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("Downloads", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("\(dramaId.replacingOccurrences(of: ":", with: "_"))_\(episode).ts")
    }

    nonisolated func isDownloaded(dramaId: String, episode: Int) -> Bool {
        FileManager.default.fileExists(atPath: localURL(dramaId: dramaId, episode: episode).path)
    }

    // MARK: - 串行泵

    private func pump(api: APIClient) async {
        guard !running else { return }
        running = true
        defer { running = false }

        while !queue.isEmpty {
            let job = queue.removeFirst()
            let id = "\(job.dramaId)#\(job.episode)"
            guard let i = tasks.firstIndex(where: { $0.id == id }), tasks[i].status != .paused else { continue }
            tasks[i].status = .running
            do {
                try await downloadHls(api: api, dramaId: job.dramaId, episode: job.episode, taskId: id)
                if let j = tasks.firstIndex(where: { $0.id == id }) {
                    tasks[j].status = .success
                    tasks[j].progress = 1
                }
            } catch {
                if let j = tasks.firstIndex(where: { $0.id == id }) {
                    tasks[j].status = .failed
                    tasks[j].error = error.localizedDescription
                }
            }
        }
    }

    /// 打开会话 → hls/open → 拉 m3u8 → 逐分片 → 合并单文件
    private func downloadHls(api: APIClient, dramaId: String, episode: Int, taskId: String) async throws {
        let open = try await api.post(PlaybackOpenResponse.self, path: Endpoints.playbackOpen, body: [
            "dramaId": dramaId,
            "resume": false,
            "fromHistory": false,
        ])
        guard let session = open.session else { throw JukuAPIError("无播放会话") }
        defer {
            Task {
                _ = try? await api.post(Bool.self, path: Endpoints.playbackControl, body: [
                    "session": session, "action": "close",
                ])
            }
        }

        let hls = try await api.post(HlsOpenResponse.self, path: Endpoints.hlsOpen, body: [
            "session": session,
            "episode": episode,
        ])
        guard let run = hls.run, let base = api.baseURL else {
            throw JukuAPIError("HLS 打开失败")
        }

        // 下载 index.m3u8
        var comps = URLComponents(url: base.appendingPathComponent(Endpoints.hlsIndex), resolvingAgainstBaseURL: false)!
        comps.queryItems = [
            URLQueryItem(name: "session", value: session),
            URLQueryItem(name: "run", value: String(run)),
        ]
        guard let m3u8URL = comps.url else { throw JukuAPIError("m3u8 URL 无效") }
        let (m3u8Data, _) = try await URLSession.shared.data(from: m3u8URL)
        let playlist = String(decoding: m3u8Data, as: UTF8.self)

        // 解析分片数量（0-based）
        var segmentCount = 0
        for line in playlist.split(separator: "\n") {
            if line.contains(".ts") || line.contains("segment") {
                segmentCount += 1
            }
        }
        if segmentCount == 0 { segmentCount = 1 }

        let out = localURL(dramaId: dramaId, episode: episode)
        FileManager.default.createFile(atPath: out.path, contents: nil)
        let handle = try FileHandle(forWritingTo: out)
        defer { try? handle.close() }

        for idx in 0..<segmentCount {
            var sc = URLComponents(url: base.appendingPathComponent("/api/ui/playback/hls/segment.ts"),
                                   resolvingAgainstBaseURL: false)!
            sc.queryItems = [
                URLQueryItem(name: "session", value: session),
                URLQueryItem(name: "run", value: String(run)),
                URLQueryItem(name: "segment", value: String(idx)),
            ]
            guard let segURL = sc.url else { continue }
            let (data, _) = try await URLSession.shared.data(from: segURL)
            try handle.write(contentsOf: data)
            if let i = tasks.firstIndex(where: { $0.id == taskId }) {
                tasks[i].progress = Double(idx + 1) / Double(segmentCount)
                tasks[i].downloadedBytes += Int64(data.count)
                tasks[i].totalBytes = tasks[i].downloadedBytes
            }
        }
    }
}
