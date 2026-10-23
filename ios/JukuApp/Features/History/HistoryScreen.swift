import SwiftUI

/// 观看历史：续播 / 删除 / 清空
struct HistoryScreen: View {
    @EnvironmentObject var app: AppSession
    @StateObject private var vm = HistoryViewModel()

    var body: some View {
        NavigationStack {
            Group {
                if vm.items.isEmpty {
                    EmptyStateView(icon: "clock", title: "暂无观看历史")
                } else {
                    List {
                        ForEach(vm.items) { item in
                            Button {
                                // fromHistory=true 仅历史入口
                                let local = DownloadManager.shared.localURL(dramaId: item.dramaId, episode: item.episode)
                                let offline = FileManager.default.fileExists(atPath: local.path)
                                PlayerRouter.shared.open(
                                    dramaId: item.dramaId,
                                    episode: item.episode,
                                    fromHistory: true,
                                    position: item.position ?? 0,
                                    offlineURL: offline ? local : nil
                                )
                            } label: {
                                VStack(alignment: .leading, spacing: 6) {
                                    Text(item.title ?? item.dramaId)
                                        .font(.headline)
                                    Text("第 \(item.episode) 集 · \(fmt(item.position ?? 0)) / \(fmt(item.duration ?? 0))")
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                    ProgressView(value: (item.position ?? 0), total: max(item.duration ?? 1, 1))
                                        .tint(.accentColor)
                                }
                                .padding(.vertical, 4)
                            }
                            .swipeActions {
                                Button(role: .destructive) {
                                    Task { await vm.remove(api: app.api, dramaId: item.dramaId) }
                                } label: { Label("删除", systemImage: "trash") }
                            }
                        }
                    }
                }
            }
            .navigationTitle("历史")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("清空", role: .destructive) {
                        Task { await vm.clear(api: app.api) }
                    }
                    .disabled(vm.items.isEmpty)
                }
            }
        }
        .task { await vm.load(api: app.api) }
    }

    private func fmt(_ s: Double) -> String {
        let t = Int(s)
        return String(format: "%d:%02d", t / 60, t % 60)
    }
}

@MainActor
final class HistoryViewModel: ObservableObject {
    @Published var items: [HistoryItem] = []

    func load(api: APIClient) async {
        if let res = try? await api.get(HistoryResponse.self, path: Endpoints.playbackHistory) {
            items = res.data ?? []
        }
    }

    func remove(api: APIClient, dramaId: String) async {
        _ = try? await api.post(Bool.self, path: Endpoints.playbackHistoryRemove, body: ["dramaId": dramaId])
        await load(api: api)
    }

    func clear(api: APIClient) async {
        _ = try? await api.post(Bool.self, path: Endpoints.playbackHistoryRemove, body: ["all": true])
        items = []
    }
}
