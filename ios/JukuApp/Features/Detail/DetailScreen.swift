import SwiftUI

/// 剧集详情：海报/简介/追剧/选集/下载面板
struct DetailScreen: View {
    @EnvironmentObject var app: AppSession
    let dramaId: String
    @StateObject private var vm = DetailViewModel()
    @State private var showDownload = false

    private let columns = [GridItem(.adaptive(minimum: 64), spacing: 8)]

    var body: some View {
        Group {
            if vm.loading {
                LoadingView()
            } else if let err = vm.error, vm.drama == nil {
                ErrorStateView(message: err) { Task { await vm.load(api: app.api, id: dramaId) } }
            } else if let d = vm.drama {
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        HStack(alignment: .top, spacing: 14) {
                            RemoteImage(urlString: d.cover)
                                .aspectRatio(3/4, contentMode: .fill)
                                .frame(width: 120)
                                .clipShape(RoundedRectangle(cornerRadius: 14))
                            VStack(alignment: .leading, spacing: 6) {
                                Text(d.title).font(.title3.bold())
                                if let score = d.score {
                                    HStack(alignment: .firstTextBaseline, spacing: 4) {
                                        Text(score).font(.title3.bold()).foregroundStyle(.orange)
                                        Text("分 · \(d.views ?? "")").font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                                Text("\(d.sourceLabel) · \(d.episodeCount) 集 · \(releaseLabel(d.releaseStatus))")
                                    .font(.footnote).foregroundStyle(.secondary)
                                Text(d.onlineDate ?? "")
                                    .font(.caption).foregroundStyle(.secondary)
                                HStack {
                                    ForEach(d.tags ?? [], id: \.self) { t in
                                        Text(t)
                                            .font(.caption2.weight(.semibold))
                                            .padding(.horizontal, 8).padding(.vertical, 3)
                                            .background(Color(.secondarySystemBackground), in: Capsule())
                                    }
                                }
                            }
                        }
                        .padding(.horizontal)

                        HStack(spacing: 10) {
                            Button {
                                Task { await play(d, episode: vm.resumeEpisode) }
                            } label: {
                                Label(vm.resumeLabel, systemImage: "play.fill")
                                    .frame(maxWidth: .infinity)
                            }
                            .buttonStyle(.borderedProminent)

                            Button(vm.followLabel) {
                                showDownload = false
                                Task { await vm.cycleFollow(api: app.api) }
                            }
                            .buttonStyle(.bordered)

                            Button("下载") { showDownload = true }
                                .buttonStyle(.bordered)
                        }
                        .padding(.horizontal)

                        Text(d.desc ?? "")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .padding(.horizontal)

                        HStack {
                            Text("分集").font(.headline)
                            Spacer()
                            Text("共 \(d.episodeCount) 集")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        .padding(.horizontal)

                        LazyVGrid(columns: columns, spacing: 8) {
                            ForEach(vm.episodes) { ep in
                                Button {
                                    Task { await play(d, episode: ep.epIndex) }
                                } label: {
                                    VStack(spacing: 2) {
                                        Text("\(ep.epIndex)")
                                            .font(.subheadline.bold())
                                        if ep.vip == true {
                                            Text("VIP").font(.system(size: 8, weight: .bold))
                                                .foregroundStyle(.orange)
                                        }
                                    }
                                    .frame(maxWidth: .infinity, minHeight: 44)
                                    .background(Color(.secondarySystemBackground),
                                                in: RoundedRectangle(cornerRadius: 10))
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.horizontal)
                        .padding(.bottom, 24)
                    }
                }
            }
        }
        .navigationTitle(vm.drama?.title ?? "详情")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(isPresented: $showDownload) {
            DownloadSheet(dramaId: dramaId, episodes: vm.episodes)
        }
        .task { await vm.load(api: app.api, id: dramaId) }
    }

    private func releaseLabel(_ s: String?) -> String {
        switch s {
        case "finished": return "已完结"
        case "ongoing": return "连载中"
        default: return "状态未知"
        }
    }

    private func play(_ d: Drama, episode: Int) async {
        let local = DownloadManager.shared.localURL(dramaId: d.id, episode: episode)
        let offline = FileManager.default.fileExists(atPath: local.path)
        PlayerRouter.shared.open(
            dramaId: d.id, episode: episode, fromHistory: false,
            offlineURL: offline ? local : nil
        )
    }
}

// PlayerRouter 已移至 PlayerScreen.swift

@MainActor
final class DetailViewModel: ObservableObject {
    @Published var drama: Drama?
    @Published var episodes: [Episode] = []
    @Published var loading = false
    @Published var error: String?
    @Published var follow: FollowingItem?
    @Published var resumeEpisode = 1

    var followLabel: String {
        if follow?.saved == true && follow?.completed != true { return "在看 ✓" }
        if follow?.completed == true { return "已看 ✓" }
        return "追剧"
    }

    var resumeLabel: String {
        resumeEpisode > 1 ? "续播 第\(resumeEpisode)集" : "立即播放"
    }

    func load(api: APIClient, id: String) async {
        loading = true
        error = nil
        defer { loading = false }
        do {
            let res = try await api.get(DramasResponse.self, path: Endpoints.dramas, query: ["refresh": "1"])
            drama = res.allDramas.first { $0.id == id }
            // 打开会话取分集
            let open = try await api.post(PlaybackOpenResponse.self, path: Endpoints.playbackOpen, body: [
                "dramaId": id,
                "resume": true,
                "fromHistory": false,
            ])
            episodes = open.episodes ?? []
            if let initEp = open.initialIndex {
                resumeEpisode = initEp
            }
            let fRes = try await api.get(FollowingResponse.self, path: Endpoints.following)
            follow = (fRes.data ?? []).first { $0.dramaId == id }
            _ = try? await api.post(Bool.self, path: Endpoints.playbackControl, body: [
                "session": open.session ?? "",
                "action": "close",
            ])
        } catch {
            self.error = (error as? ApiErrorBody)?.error ?? error.localizedDescription
        }
    }

    func cycleFollow(api: APIClient) async {
        do {
            let next: [String: Any]
            if follow == nil {
                next = ["dramaId": drama?.id ?? "", "saved": true, "completed": false, "acknowledge": true]
            } else if follow?.completed == true {
                next = ["dramaId": drama?.id ?? "", "saved": false, "completed": false, "acknowledge": true]
            } else if follow?.saved == true {
                next = ["dramaId": drama?.id ?? "", "saved": true, "completed": true, "acknowledge": true]
            } else {
                next = ["dramaId": drama?.id ?? "", "saved": true, "completed": false, "acknowledge": true]
            }
            _ = try await api.post(Bool.self, path: Endpoints.following, body: next)
            let fRes = try await api.get(FollowingResponse.self, path: Endpoints.following)
            follow = (fRes.data ?? []).first { $0.dramaId == drama?.id }
        } catch {
            // ignore
        }
    }
}
