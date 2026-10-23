import SwiftUI

@main
struct JukuAppApp: App {
    @StateObject private var app = AppSession()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(app)
                .preferredColorScheme(app.themeMode.colorScheme)
        }
    }
}
