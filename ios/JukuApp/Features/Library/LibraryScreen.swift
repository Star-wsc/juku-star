import SwiftUI

/// 剧库首页 — 站源 chip / 类型 / 分类 / 排序 / VIP / 三列网格
struct LibraryScreen: View {
    @EnvironmentObject var app: AppSession
    @StateObject private var vm = LibraryViewModel()

    private let columns = [
        GridItem(.flexible(), spacing: 10),
        GridItem(.flexible(), spacing: 10),
        GridItem(.flexible(), spacing: 10),
    ]

    var body: some View {
        NavigationStack {
            Group {
                if vm.loading && vm.dramas.isEmpty {
                    LoadingView()
                } else if let err = vm.error, vm.dramas.isEmpty {
                    ErrorStateView(message: err) { Task { await vm.load(api: app.api) } }
                } else {
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 12) {
                            filterBar
                            if vm.dramas.isEmpty {
                                EmptyStateView(icon: "film.stack",
                                               title: "没有符合条件的剧集",
                                               subtitle: "试试切换站源或关闭 VIP 过滤")
                            } else {
                                LazyVGrid(columns: columns, spacing: 14) {
                                    ForEach(vm.visible) { d in
                                        NavigationLink(value: d.id) {
                                            DramaPoster(drama: d)
                                        }
                                        .buttonStyle(.plain)
                                        .onAppear {
                                            if d.id == vm.visible.last?.id {
                                                vm.loadMore()
                                            }
                                        }
                                    }
                                }
                                .padding(.horizontal, 12)

                                if vm.hasMore {
                                    ProgressView()
                                        .frame(maxWidth: .infinity)
                                        .padding()
                                }
                            }
                        }
                    }
                    .refreshable { await vm.refresh(api: app.api) }
                }
            }
            .navigationTitle("短剧库")
            .navigationDestination(for: String.self) { id in
                DetailScreen(dramaId: id)
            }
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    NavigationLink {
                        SearchScreen()
                    } label: {
                        Image(systemName: "magnifyingglass")
                    }
                }
            }
        }
        .task {
            vm.enabledSources = app.enabledSources
            await vm.load(api: app.api)
        }
        .playerHost()
        .onChange(of: app.enabledSources) { _, new in
            vm.enabledSources = new
            vm.applyFilters()
        }
    }

    private var filterBar: some View {
        VStack(alignment: .leading, spacing: 10) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    chip("全部", selected: vm.sourceFilter == "all") { vm.sourceFilter = "all"; vm.applyFilters() }
                    chip("红果", selected: vm.sourceFilter == "hongguo") { vm.sourceFilter = "hongguo"; vm.applyFilters() }
                    chip("黄果", selected: vm.sourceFilter == "huangguo") { vm.sourceFilter = "huangguo"; vm.applyFilters() }
                    chip("黄豆", selected: vm.sourceFilter == "huangdou") { vm.sourceFilter = "huangdou"; vm.applyFilters() }
                }
                .padding(.horizontal, 12)
            }

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    Menu {
                        Button("全部类型") { vm.genre = nil; vm.applyFilters() }
                        Button("真人剧") { vm.genre = "short_play"; vm.applyFilters() }
                        Button("漫剧") { vm.genre = "comic_series"; vm.applyFilters() }
                        Button("AI 剧") { vm.genre = "ai_series"; vm.applyFilters() }
                    } label: {
                        pill(vm.genreLabel)
                    }
                    Menu {
                        Button("全部分类") { vm.category = nil; vm.applyFilters() }
                        ForEach(vm.categories, id: \.self) { c in
                            Button(c) { vm.category = c; vm.applyFilters() }
                        }
                    } label: {
                        pill(vm.category ?? "分类")
                    }
                    Menu {
                        Button("默认") { vm.sort = .default; vm.applyFilters() }
                        Button("名称") { vm.sort = .name; vm.applyFilters() }
                        Button("日期") { vm.sort = .date; vm.applyFilters() }
                        Button("播放量") { vm.sort = .views; vm.applyFilters() }
                    } label: {
                        pill(vm.sort.label)
                    }
                    Button {
                        vm.hideVip.toggle()
                        vm.applyFilters()
                    } label: {
                        pill(vm.hideVip ? "显示 VIP" : "隐藏 VIP", on: vm.hideVip)
                    }
                }
                .padding(.horizontal, 12)
            }
        }
    }

    private func chip(_ t: String, selected: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(t)
                .font(.subheadline.weight(.semibold))
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .background(selected ? Color.primary.opacity(0.9) : Color(.secondarySystemBackground),
                            in: Capsule())
                .foregroundStyle(selected ? Color(.systemBackground) : .primary)
        }
    }

    private func pill(_ t: String, on: Bool = false) -> some View {
        Text(t)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .background(on ? Color.accentColor.opacity(0.18) : Color(.secondarySystemBackground), in: Capsule())
    }
}

enum DramaSort: String {
    case `default`, name, date, views
    var label: String {
        switch self {
        case .default: return "排序"
        case .name: return "名称"
        case .date: return "日期"
        case .views: return "播放量"
        }
    }
}

@MainActor
final class LibraryViewModel: ObservableObject {
    @Published var dramas: [Drama] = []
    @Published var visible: [Drama] = []
    @Published var loading = false
    @Published var error: String?
    @Published var sourceFilter = "all"
    @Published var genre: String?
    @Published var category: String?
    @Published var sort: DramaSort = .default
    @Published var hideVip = false
    @Published var enabledSources: Set<String> = ["hongguo", "huangguo", "huangdou"]
    @Published var hasMore = false

    private var page = 0

    var genreLabel: String {
        switch genre {
        case "short_play": return "真人剧"
        case "comic_series": return "漫剧"
        case "ai_series": return "AI 剧"
        default: return "类型"
        }
    }

    var categories: [String] {
        Array(Set(dramas.map { $0.category ?? "" }.filter { !$0.isEmpty })).sorted()
    }

    func load(api: APIClient) async {
        loading = true
        error = nil
        defer { loading = false }
        do {
            var query: [String: String] = ["refresh": "1"]
            if sourceFilter != "all" { query["source"] = sourceFilter }
            let res = try await api.get(DramasResponse.self, path: Endpoints.dramas, query: query)
            dramas = res.allDramas
            applyFilters()
        } catch {
            self.error = Self.message(error)
        }
    }

    func refresh(api: APIClient) async {
        await load(api: api)
    }

    func loadMore() {
        page += 1
        applyFilters()
    }

    func applyFilters() {
        var list = SourceGroup.filter(dramas, enabled: enabledSources, sourceFilter: sourceFilter)
        if hideVip { list = list.filter { !$0.isVip } }
        if let category { list = list.filter { $0.category == category } }
        if let genre {
            list = list.filter { d in
                let cat = d.category ?? ""
                let tags = d.tags ?? []
                switch genre {
                case "ai_series":
                    return cat == "AI" || tags.contains("AI")
                case "comic_series":
                    return (cat == "玄幻" || cat == "古装") && d.sourceGroup == "huangguo"
                default:
                    return cat != "AI"
                }
            }
        }
        switch sort {
        case .name: list.sort { $0.title < $1.title }
        case .date: list.sort { ($0.onlineDate ?? "") > ($1.onlineDate ?? "") }
        case .views:
            list.sort { viewsNum($0.views) > viewsNum($1.views) }
        default: break
        }
        dramas = dramas // keep raw
        let limit = 18 * (page + 1)
        visible = Array(list.prefix(limit))
        hasMore = list.count > visible.count
    }

    private func viewsNum(_ v: String?) -> Double {
        guard let v else { return 0 }
        if v.contains("亿") { return (Double(v.replacingOccurrences(of: "亿", with: "")) ?? 0) * 1e8 }
        if v.contains("万") { return (Double(v.replacingOccurrences(of: "万", with: "")) ?? 0) * 1e4 }
        return Double(v) ?? 0
    }

    private static func message(_ e: Error) -> String {
        (e as? ApiErrorBody)?.error ?? (e as? JukuAPIError)?.errorDescription ?? e.localizedDescription
    }
}
