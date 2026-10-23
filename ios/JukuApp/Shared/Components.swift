import SwiftUI

/// 海报卡片（AsyncImage 走 /api/ui/image）
struct DramaPoster: View {
    let drama: Drama
    var width: CGFloat? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ZStack(alignment: .topLeading) {
                RemoteImage(urlString: drama.cover)
                    .aspectRatio(3/4, contentMode: .fill)
                    .frame(maxWidth: .infinity)
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                    .overlay(alignment: .topTrailing) {
                        if drama.isVip {
                            Text("VIP")
                                .font(.caption2.bold())
                                .padding(.horizontal, 5)
                                .padding(.vertical, 2)
                                .background(Color.orange, in: Capsule())
                                .padding(6)
                        }
                    }
                    .overlay(alignment: .bottomLeading) {
                        Text(drama.sourceLabel)
                            .font(.caption2.bold())
                            .padding(.horizontal, 5)
                            .padding(.vertical, 2)
                            .background(.black.opacity(0.5), in: Capsule())
                            .foregroundStyle(.white)
                            .padding(6)
                    }
            }
            Text(drama.title)
                .font(.caption.weight(.semibold))
                .lineLimit(2)
                .foregroundStyle(.primary)
            Text("\(drama.episodeCount)集 · \(drama.category ?? "")")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .frame(width: width)
    }
}

/// 带 Cookie 的远程图（自定义 loader 确保会话 Cookie）
struct RemoteImage: View {
    let urlString: String?
    @EnvironmentObject var app: AppSession
    @State private var image: UIImage?

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image)
                    .resizable()
            } else {
                Rectangle()
                    .fill(Color.gray.opacity(0.15))
                    .overlay(ProgressView())
            }
        }
        .task(id: urlString) {
            await load()
        }
    }

    private func load() async {
        guard let urlString, image == nil,
              let url = APIClient.imageURL(raw: urlString, base: app.baseURL) else { return }
        var req = URLRequest(url: url)
        req.setValue("none", forHTTPHeaderField: "Sec-Fetch-Site")
        if let vid = APIClient.sharedViewerId {
            req.setValue(vid, forHTTPHeaderField: "X-Juku-Viewer")
        }
        if let (data, _) = try? await URLSession.shared.data(for: req),
           let img = UIImage(data: data) {
            image = img
        }
    }
}

struct EmptyStateView: View {
    let icon: String
    let title: String
    var subtitle: String?

    var body: some View {
        ContentUnavailableView {
            Label(title, systemImage: icon)
        } description: {
            if let subtitle { Text(subtitle) }
        }
    }
}

struct LoadingView: View {
    var body: some View {
        ProgressView("加载中…")
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct ErrorStateView: View {
    let message: String
    var retry: (() -> Void)?

    var body: some View {
        ContentUnavailableView {
            Label("出错了", systemImage: "exclamationmark.triangle")
        } description: {
            Text(message)
        } actions: {
            if let retry {
                Button("重试", action: retry)
                    .buttonStyle(.borderedProminent)
            }
        }
    }
}
