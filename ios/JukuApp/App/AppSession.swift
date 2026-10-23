import Foundation
import Combine

/// 全局会话：对应 Android 的 MainViewModel + 启动鉴权流程
@MainActor
final class AppSession: ObservableObject {
    @Published var baseURLString: String = Prefs.baseURLString
    @Published var viewer: Viewer?
    @Published var account: Account?
    @Published var showAuth: Bool = true
    @Published var themeMode: ThemeMode = Prefs.themeMode
    @Published var enabledSources: Set<String> = Prefs.enabledSources
    @Published var toast: String?

    let api = APIClient()

    init() {
        // 已有服务器配置则尝试恢复身份
        if !baseURLString.isEmpty {
            Task { await bootstrap() }
        }
    }

    var baseURL: URL? {
        APIClient.normalizeBaseURL(baseURLString)
    }

    func applyBaseURL(_ raw: String) {
        baseURLString = raw
        Prefs.baseURLString = raw
        api.baseURL = APIClient.normalizeBaseURL(raw)
    }

    /// 首次/重连：GET /api/ui/viewer 建立匿名身份
    func bootstrap() async {
        guard let url = baseURL else {
            showAuth = true
            return
        }
        api.baseURL = url
        do {
            // 引导：无 confirm 下发 Cookie
            _ = try await api.get(ViewerBootResponse.self, path: "/api/ui/viewer")
            let v = try await api.get(Viewer.self, path: "/api/ui/viewer", query: ["confirm": "1"])
            viewer = v
            account = v.account
            if v.requireLogin == true && v.account == nil {
                showAuth = true
            } else {
                showAuth = false
            }
        } catch {
            showAuth = true
        }
    }

    /// 登录/注册前：logout + 清 Cookie（iOS 文档 §6.1）
    func clearSessionCookies() async {
        HTTPCookieStorage.shared.cookies?.forEach {
            HTTPCookieStorage.shared.deleteCookie($0)
        }
    }

    func login(username: String, password: String) async throws {
        await clearSessionCookies()
        _ = try? await api.post(Bool.self, path: "/api/ui/account/logout", body: [:])
        let res = try await api.post(LoginResponse.self, path: "/api/ui/account/login", body: [
            "username": username,
            "password": password, // 禁止 trim
        ])
        account = res.account
        await bootstrap()
        showAuth = false
    }

    func register(username: String, password: String) async throws {
        await clearSessionCookies()
        _ = try? await api.post(Bool.self, path: "/api/ui/account/logout", body: [:], allowEmpty: true)
        let res = try await api.post(LoginResponse.self, path: "/api/ui/account/register", body: [
            "username": username,
            "password": password,
        ], statusCode: 201)
        account = res.account
        await bootstrap()
        showAuth = false
    }

    func logout() async {
        _ = try? await api.post(Bool.self, path: "/api/ui/account/logout", body: [:])
        await clearSessionCookies()
        account = nil
        showAuth = true
        _ = try? await api.get(ViewerBootResponse.self, path: "/api/ui/viewer")
    }

    func changePassword(current: String, new: String) async throws {
        _ = try await api.post(Bool.self, path: "/api/ui/account/password", body: [
            "password": current,
            "newPassword": new,
        ])
    }

    func connectGuest() async throws {
        await clearSessionCookies()
        try await bootstrap()
        if viewer != nil { showAuth = false }
    }

    func flash(_ message: String) {
        toast = message
        Task {
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            if toast == message { toast = nil }
        }
    }

    func setTheme(_ mode: ThemeMode) {
        themeMode = mode
        Prefs.themeMode = mode
    }

    func toggleSource(_ group: String) {
        if enabledSources.contains(group) {
            // 至少保留一个
            if enabledSources.count > 1 { enabledSources.remove(group) }
        } else {
            enabledSources.insert(group)
        }
        Prefs.enabledSources = enabledSources
    }
}

enum ThemeMode: String, CaseIterable {
    case system, light, dark

    var label: String {
        switch self {
        case .system: return "跟随系统"
        case .light: return "浅色"
        case .dark: return "深色"
        }
    }

    var colorScheme: ColorScheme? {
        switch self {
        case .system: return nil
        case .light: return .light
        case .dark: return .dark
        }
    }
}
