import Foundation

/// 本地镜像缓存（历史/追剧离线可见）— 以服务端为准
enum LocalStore {
    private static let d = UserDefaults.standard

    static func saveHistoryMirror(_ items: [[String: Any]]) {
        d.set(items, forKey: "historyMirror")
    }

    static func loadHistoryMirror() -> [[String: Any]] {
        d.array(forKey: "historyMirror") as? [[String: Any]] ?? []
    }

    static func clearAll() {
        d.removeObject(forKey: "historyMirror")
    }
}
