import Foundation

/// UserDefaults 偏好 — 对齐 Android SharedPreferences
enum Prefs {
    private static let d = UserDefaults.standard

    static var baseURLString: String {
        get { d.string(forKey: "baseURL") ?? "" }
        set { d.set(newValue, forKey: "baseURL") }
    }

    static var themeMode: ThemeMode {
        get { ThemeMode(rawValue: d.string(forKey: "theme") ?? "system") ?? .system }
        set { d.set(newValue.rawValue, forKey: "theme") }
    }

    static var enabledSources: Set<String> {
        get {
            let arr = d.stringArray(forKey: "sources") ?? ["hongguo", "huangguo", "huangdou"]
            return Set(arr)
        }
        set { d.set(Array(newValue), forKey: "sources") }
    }

    static var defaultQuality: String {
        get { d.string(forKey: "quality") ?? "高清" }
        set { d.set(newValue, forKey: "quality") }
    }

    static var defaultSpeed: Double {
        get { d.double(forKey: "speed") == 0 ? 1.0 : d.double(forKey: "speed") }
        set { d.set(newValue, forKey: "speed") }
    }

    static var autoNext: Bool {
        get { d.object(forKey: "autoNext") as? Bool ?? true }
        set { d.set(newValue, forKey: "autoNext") }
    }

    static var prefetch: Bool {
        get { d.object(forKey: "prefetch") as? Bool ?? true }
        set { d.set(newValue, forKey: "prefetch") }
    }

    static var disclaimerAccepted: Bool {
        get { d.bool(forKey: "disclaimer") }
        set { d.set(newValue, forKey: "disclaimer") }
    }
}
