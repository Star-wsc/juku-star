import Foundation

/// 源码分组归并 — 与 Android 前缀匹配一致
enum SourceGroup {
    static func group(of source: String?) -> String {
        let s = (source ?? "").lowercased()
        if s.hasPrefix("hongguo") { return "hongguo" }
        if s.hasPrefix("huangguo") || s == "cloudfront" { return "huangguo" }
        if s.hasPrefix("huangdou") { return "huangdou" }
        return s
    }

    static func label(of source: String?) -> String {
        switch group(of: source) {
        case "hongguo": return "红果"
        case "huangguo": return "黄果"
        case "huangdou": return "黄豆"
        default: return source ?? "未知"
        }
    }

    static let all = ["hongguo", "huangguo", "huangdou"]

    static func filter(_ dramas: [Drama], enabled: Set<String>, sourceFilter: String?) -> [Drama] {
        dramas.filter { d in
            let g = d.sourceGroup
            guard enabled.contains(g) else { return false }
            if let f = sourceFilter, f != "all", g != f { return false }
            return true
        }
    }
}
