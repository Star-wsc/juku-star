import SwiftUI

/// 下载选集面板（详情页 sheet）
struct DownloadSheet: View {
    @EnvironmentObject var app: AppSession
    @Environment(\.dismiss) private var dismiss
    let dramaId: String
    let episodes: [Episode]

    @State private var selected = Set<Int>()
    @State private var quality = "高清"

    var body: some View {
        NavigationStack {
            VStack(spacing: 14) {
                Picker("画质", selection: $quality) {
                    Text("高清").tag("高清")
                    Text("标清").tag("标清")
                    Text("流畅").tag("流畅")
                }
                .pickerStyle(.segmented)

                Text("串行下载（避免打满 4 播放会话）· HLS 分片合并单文件")
                    .font(.footnote)
                    .foregroundStyle(.secondary)

                ScrollView {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 70))], spacing: 8) {
                        ForEach(episodes) { ep in
                            Button {
                                if selected.contains(ep.epIndex) { selected.remove(ep.epIndex) }
                                else { selected.insert(ep.epIndex) }
                            } label: {
                                Text(ep.displayTitle)
                                    .font(.caption.bold())
                                    .frame(maxWidth: .infinity, minHeight: 42)
                                    .background(selected.contains(ep.epIndex)
                                                ? Color.accentColor
                                                : Color(.secondarySystemBackground),
                                                in: RoundedRectangle(cornerRadius: 10))
                                    .foregroundStyle(selected.contains(ep.epIndex) ? .white : .primary)
                            }
                        }
                    }
                    .padding(.vertical)
                }

                Button {
                    Task {
                        for ep in selected.sorted() {
                            await DownloadManager.shared.enqueue(dramaId: dramaId, episode: ep, api: app.api)
                        }
                        app.flash("已加入 \(selected.count) 个下载任务")
                        dismiss()
                    }
                } label: {
                    Text("开始下载（\(selected.count)）")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .disabled(selected.isEmpty)
            }
            .padding()
            .navigationTitle("下载选集")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("关闭") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}
