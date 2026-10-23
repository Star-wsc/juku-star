import SwiftUI

/// 追剧：想看 / 在看 / 已看 + 新集提示
struct FollowingScreen: View {
    @EnvironmentObject var app: AppSession
    @StateObject private var vm = FollowingViewModel()
    @State private var tab = 0

    var body: some View {
        NavigationStack {
            VStack(spacing: 10) {
                Picker("", selection: $tab) {
                    Text("想看").tag(0)
                    Text("在看").tag(1)
                    Text("已看").tag(2)
                }
                .pickerStyle(.segmented)
                .padding(.horizontal)

                if vm.items.isEmpty {
                    EmptyStateView(icon: "heart", title: "暂无追剧", subtitle: "在详情页添加")
                } else {
                    List {
                        ForEach(vm.filtered(tab: tab)) { item in
                            NavigationLink(value: item.dramaId) {
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(item.title ?? item.dramaId)
                                        .font(.headline)
                                    Text("\(SourceGroup.label(of: item.source)) · \(item.totalEpisode ?? 0) 集")
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                    if (item.newEpisodes ?? 0) > 0 {
                                        Text("更新至 · 新增 \(item.newEpisodes!) 集")
                                            .font(.caption.weight(.semibold))
                                            .foregroundStyle(Color.accentColor)
                                    }
                                }
                                .padding(.vertical, 2)
                            }
                            .swipeActions {
                                Button(role: .destructive) {
                                    Task { await vm.remove(api: app.api, dramaId: item.dramaId) }
                                } label: { Label("移除", systemImage: "trash") }
                            }
                        }
                    }
                }
            }
            .navigationTitle("追剧")
            .navigationDestination(for: String.self) { DetailScreen(dramaId: $0) }
        }
        .task { await vm.load(api: app.api) }
    }
}

@MainActor
final class FollowingViewModel: ObservableObject {
    @Published var items: [FollowingItem] = []

    func load(api: APIClient) async {
        if let res = try? await api.get(FollowingResponse.self, path: Endpoints.following) {
            items = res.data ?? []
        }
    }

    func filtered(tab: Int) -> [FollowingItem] {
        // saved + !completed → 在看/想看（按加入时间）；completed → 已看
        switch tab {
        case 0:
            return items.filter { $0.saved == true && $0.completed != true }
                .sorted { ($0.addedAt ?? 0) > ($1.addedAt ?? 0) }
        case 1:
            return items.filter { $0.saved == true && $0.completed != true && ($0.knownEpisodes ?? 0) > 0 }
                .sorted { ($0.updatedAt ?? 0) > ($1.updatedAt ?? 0) }
        default:
            return items.filter { $0.completed == true }
                .sorted { ($0.updatedAt ?? 0) > ($1.updatedAt ?? 0) }
        }
    }

    func remove(api: APIClient, dramaId: String) async {
        _ = try? await api.post(Bool.self, path: Endpoints.following, body: [
            "dramaId": dramaId,
            "saved": false,
            "completed": false,
            "acknowledge": true,
        ])
        await load(api: api)
    }
}
