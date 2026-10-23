import SwiftUI
import AVKit

/// 全屏播放页 — AVPlayer + 手势（亮度/音量/进度）+ 选集抽屉 + 离线
struct PlayerScreen: View {
    @EnvironmentObject var app: AppSession
    @Environment(\.dismiss) private var dismiss
    @StateObject private var pc = PlaybackController()

    let dramaId: String
    let episode: Int
    let fromHistory: Bool
    let startPosition: Double
    var offlineURL: URL? = nil

    @State private var showEpisodes = false
    @State private var hudText: String?
    @State private var brightness: Double = 0.7
    @State private var volume: Double = 0.5

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()

            VStack(spacing: 0) {
                videoArea
                metaArea
                Spacer(minLength: 0)
            }

            if let hudText {
                Text(hudText)
                    .font(.subheadline.weight(.bold))
                    .padding(12)
                    .background(.black.opacity(0.7), in: RoundedRectangle(cornerRadius: 12))
                    .foregroundStyle(.white)
                    .transition(.opacity)
            }

            if showEpisodes {
                VStack {
                    Spacer()
                    episodeDrawer
                }
                .transition(.move(edge: .bottom))
            }
        }
        .navigationTitle("播放")
        .navigationBarTitleDisplayMode(.inline)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .statusBarHidden()
        .task {
            if let offlineURL {
                await pc.openOffline(url: offlineURL, episode: episode, title: "第\(episode)集")
            } else {
                await pc.open(api: app.api, dramaId: dramaId, episode: episode,
                              fromHistory: fromHistory, position: startPosition)
            }
            pc.showControlsTemporarily()
        }
        .onDisappear { Task { await pc.close() } }
        .animation(.spring(response: 0.3, dampingFraction: 0.85), value: showEpisodes)
        .animation(.easeOut(duration: 0.2), value: hudText)
    }

    // MARK: - 视频 + 手势

    private var videoArea: some View {
        ZStack {
            if let player = pc.player {
                VideoPlayer(player: player)
            } else {
                Color.black
                    .overlay(ProgressView().tint(.white))
            }

            // 手势层：左亮度 / 右音量 / 中进度
            GeometryReader { geo in
                HStack(spacing: 0) {
                    gestureZone(width: geo.size.width * 0.28, zone: .brightness)
                    gestureZone(width: geo.size.width * 0.44, zone: .seek)
                    gestureZone(width: geo.size.width * 0.28, zone: .volume)
                }
            }

            if pc.showControls {
                controlsOverlay
            }
        }
        .aspectRatio(16/9, contentMode: .fit)
        .brightness(brightness - 0.7)
    }

    private enum GZone { case brightness, volume, seek }

    private func gestureZone(width: CGFloat, zone: GZone) -> some View {
        Color.clear
            .frame(width: width)
            .contentShape(Rectangle())
            .gesture(
                DragGesture(minimumDistance: 8)
                    .onChanged { v in
                        pc.showControlsTemporarily()
                        switch zone {
                        case .brightness:
                            let delta = -v.translation.height / 220
                            brightness = min(1, max(0, 0.7 + delta))
                            hudText = "亮度 \(Int(brightness * 100))%"
                        case .volume:
                            let delta = -v.translation.height / 220
                            volume = min(1, max(0, 0.5 + delta))
                            hudText = "音量 \(Int(volume * 100))%"
                            // AVPlayer 音量
                            pc.player?.volume = Float(volume)
                        case .seek:
                            let delta = v.translation.width * 0.35
                            let target = min(max(0, startPositionOfSeek + delta), max(pc.duration, 1))
                            pendingSeek = target
                            hudText = String(format: "%@%d:%02d", delta >= 0 ? "+" : "",
                                             Int(target) / 60, Int(target) % 60)
                        }
                    }
                    .onEnded { _ in
                        if zone == .seek, let t = pendingSeek {
                            pc.seek(to: t)
                            pendingSeek = nil
                        }
                        DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { hudText = nil }
                    }
            )
            .simultaneousGesture(
                TapGesture(count: 2).onEnded {
                    if zone == .seek { pc.togglePlay() }
                    else if zone == .brightness { pc.seek(to: max(0, pc.position - 10)); flashHUD("−10s") }
                    else { pc.seek(to: min(pc.duration, pc.position + 10)); flashHUD("+10s") }
                }
            )
            .onTapGesture {
                if pc.showControls { pc.showControls = false }
                else { pc.showControlsTemporarily() }
            }
    }

    @State private var pendingSeek: Double?
    @State private var seekBase: Double = 0

    private var startPositionOfSeek: Double { pendingSeek ?? pc.position }

    private func flashHUD(_ t: String) {
        hudText = t
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { hudText = nil }
    }

    // MARK: - 控制层

    private var controlsOverlay: some View {
        VStack {
            HStack {
                Button { pc.showControls = false } label: {
                    Image(systemName: "chevron.down")
                }
                Spacer()
                Text(pc.offlineMode ? "离线" : "run=\(pc.run)")
                    .font(.caption.bold())
                    .padding(.horizontal, 8).padding(.vertical, 4)
                    .background(.white.opacity(0.15), in: Capsule())
                Spacer()
                Button("选集") { showEpisodes = true }
            }
            .foregroundStyle(.white)
            .padding()

            Spacer()

            HStack(spacing: 40) {
                Button { Task { await pc.switchEpisode(max(1, pc.currentEpisode - 1)) } } label: {
                    Image(systemName: "backward.end.fill").font(.title2)
                }
                Button { pc.togglePlay() } label: {
                    Image(systemName: pc.isPlaying ? "pause.fill" : "play.fill")
                        .font(.system(size: 34, weight: .bold))
                        .frame(width: 68, height: 68)
                        .background(.white.opacity(0.18), in: Circle())
                }
                Button { Task { await pc.switchEpisode(pc.currentEpisode + 1) } } label: {
                    Image(systemName: "forward.end.fill").font(.title2)
                }
            }
            .foregroundStyle(.white)
            .padding()

            Spacer()

            VStack(spacing: 10) {
                Slider(
                    value: Binding(get: { pc.position }, set: { pc.seek(to: $0) }),
                    in: 0...max(pc.duration, 1)
                )
                HStack {
                    Text(fmt(pc.position))
                    Spacer()
                    Text(fmt(pc.duration))
                }
                .font(.caption.monospacedDigit())
                HStack {
                    Menu {
                        ForEach([0.5, 0.75, 1.0, 1.25, 1.5, 2.0, 3.0], id: \.self) { s in
                            Button(String(format: "%gx", s)) { pc.setSpeed(s) }
                        }
                    } label: {
                        Text(String(format: "%gx", pc.speed))
                    }
                    Spacer()
                    Menu {
                        ForEach(["流畅", "标清", "高清", "超清"], id: \.self) { q in
                            Button(q) { pc.qualityLabel = q }
                        }
                    } label: {
                        Text(pc.qualityLabel)
                    }
                    Spacer()
                    if !pc.offlineMode {
                        Button("下载本集") {
                            Task {
                                await DownloadManager.shared.enqueue(
                                    dramaId: dramaId, episode: pc.currentEpisode, api: app.api
                                )
                                app.flash("已加入下载")
                            }
                        }
                    }
                }
                .font(.caption)
            }
            .foregroundStyle(.white)
            .padding()
            .background(.black.opacity(0.45))
        }
        .background(.black.opacity(0.28))
    }

    private var metaArea: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("第\(pc.currentEpisode)集")
                .font(.headline)
            Text(fromHistory ? "历史续播 fromHistory=true" : (pc.offlineMode ? "本地文件播放" : "Native HLS · AVPlayer"))
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(pc.statusText)
                .font(.caption2.monospacedDigit())
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
    }

    private var episodeDrawer: some View {
        VStack(spacing: 0) {
            HStack {
                Text("选集").font(.headline)
                Spacer()
                Button("关闭") { showEpisodes = false }
            }
            .padding()
            ScrollView {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 64))], spacing: 8) {
                    ForEach(pc.episodes) { ep in
                        Button {
                            Task {
                                await pc.switchEpisode(ep.epIndex)
                                showEpisodes = false
                            }
                        } label: {
                            Text("\(ep.epIndex)")
                                .font(.subheadline.bold())
                                .frame(maxWidth: .infinity, minHeight: 42)
                                .background(ep.epIndex == pc.currentEpisode
                                            ? Color.accentColor
                                            : Color(.secondarySystemBackground),
                                            in: RoundedRectangle(cornerRadius: 10))
                                .foregroundStyle(ep.epIndex == pc.currentEpisode ? .white : .primary)
                        }
                    }
                }
                .padding()
            }
            .frame(maxHeight: 300)
        }
        .background(.ultraThinMaterial)
    }

    private func fmt(_ s: Double) -> String {
        let t = Int(max(0, s))
        return String(format: "%d:%02d", t / 60, t % 60)
    }
}

/// 播放入口宿主 — 监听 PlayerRouter
struct PlayerHost: ViewModifier {
    @StateObject private var router = PlayerRouter.shared
    @State private var presented = false

    func body(content: Content) -> some View {
        content
            .onChange(of: router.request?.dramaId) { _, _ in
                presented = router.request != nil
            }
            .fullScreenCover(isPresented: $presented) {
                if let req = router.request {
                    NavigationStack {
                        PlayerScreen(
                            dramaId: req.dramaId,
                            episode: req.episode,
                            fromHistory: req.fromHistory,
                            startPosition: req.position,
                            offlineURL: req.offlineURL
                        )
                    }
                    .onDisappear { router.request = nil }
                }
            }
    }
}

extension View {
    func playerHost() -> some View { modifier(PlayerHost()) }
}

/// 简易全局播放路由
final class PlayerRouter: ObservableObject {
    static let shared = PlayerRouter()
    struct Req: Equatable {
        var dramaId: String
        var episode: Int
        var fromHistory: Bool
        var position: Double
        var offlineURL: URL?
    }
    @Published var request: Req?

    func open(dramaId: String, episode: Int, fromHistory: Bool, position: Double = 0, offlineURL: URL? = nil) {
        request = Req(dramaId: dramaId, episode: episode, fromHistory: fromHistory,
                      position: position, offlineURL: offlineURL)
    }
}
