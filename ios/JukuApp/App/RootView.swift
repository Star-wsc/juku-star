import SwiftUI

struct RootView: View {
    @EnvironmentObject var app: AppSession

    var body: some View {
        Group {
            if app.showAuth {
                AuthScreen()
            } else {
                MainTabView()
            }
        }
        .overlay(alignment: .bottom) {
            if let t = app.toast {
                Text(t)
                    .font(.subheadline.weight(.semibold))
                    .padding(.horizontal, 16)
                    .padding(.vertical, 12)
                    .background(.ultraThinMaterial, in: Capsule())
                    .padding(.bottom, 80)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.85), value: app.toast)
    }
}

struct MainTabView: View {
    @EnvironmentObject var app: AppSession
    @State private var tab = 0

    var body: some View {
        TabView(selection: $tab) {
            LibraryScreen()
                .tabItem { Label("剧库", systemImage: "house") }
                .tag(0)
            FollowingScreen()
                .tabItem { Label("追剧", systemImage: "heart") }
                .tag(1)
            DownloadsScreen()
                .tabItem { Label("下载", systemImage: "arrow.down.circle") }
                .tag(2)
            HistoryScreen()
                .tabItem { Label("历史", systemImage: "clock") }
                .tag(3)
            SettingsScreen()
                .tabItem { Label("设置", systemImage: "gearshape") }
                .tag(4)
        }
        .playerHost()
    }
}
