import SwiftUI

/// 本地 + 联网搜索
struct SearchScreen: View {
    @EnvironmentObject var app: AppSession
    @State private var q = ""
    @State private var results: [Drama] = []
    @State private var remote = false
    @State private var searching = false

    private let columns = [
        GridItem(.flexible(), spacing: 10),
        GridItem(.flexible(), spacing: 10),
        GridItem(.flexible(), spacing: 10),
    ]

    var body: some View {
        VStack(spacing: 10) {
            HStack {
                Image(systemName: "magnifyingglass")
                TextField("搜索剧名 / 标签", text: $q)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .onSubmit { Task { await search() } }
            }
            .padding(10)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
            .padding(.horizontal)

            Picker("", selection: $remote) {
                Text("本地缓存").tag(false)
                Text("联网搜索").tag(true)
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)

            if searching {
                ProgressView()
            } else if q.isEmpty {
                EmptyStateView(icon: "magnifyingglass",
                               title: "输入关键词搜索",
                               subtitle: remote ? "GET /api/ui/search" : "本地已缓存剧库")
            } else if results.isEmpty {
                EmptyStateView(icon: "questionmark.circle", title: "没有找到「\(q)」")
            } else {
                ScrollView {
                    LazyVGrid(columns: columns, spacing: 14) {
                        ForEach(results) { d in
                            NavigationLink(value: d.id) {
                                DramaPoster(drama: d)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.horizontal, 12)
                }
            }
        }
        .navigationTitle("搜索")
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(for: String.self) { DetailScreen(dramaId: $0) }
        .onChange(of: q) { _, _ in Task { await search() } }
        .onChange(of: remote) { _, _ in Task { await search() } }
    }

    private func search() async {
        let key = q.trimmingCharacters(in: .whitespaces)
        guard !key.isEmpty else { results = []; return }
        searching = true
        defer { searching = false }
        do {
            if remote {
                let res = try await app.api.get(SearchResponse.self, path: Endpoints.search, query: ["q": key])
                results = res.data ?? []
            } else {
                // 本地：若有缓存列表则过滤；演示时再拉一次 dramas
                let res = try await app.api.get(DramasResponse.self, path: Endpoints.dramas, query: [:])
                let all = res.allDramas
                results = all.filter {
                    (($0.title + ($0.desc ?? "") + ($0.tags ?? []).joined())).localizedCaseInsensitiveContains(key)
                }
            }
        } catch {
            results = []
            app.flash((error as? ApiErrorBody)?.error ?? "搜索失败")
        }
    }
}
