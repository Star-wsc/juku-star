import Foundation

// MARK: - 对齐《API 速查表》DTO

struct ViewerBootResponse: Codable {
    let ready: Bool?
}

struct Viewer: Codable {
    let ready: Bool?
    let id: String?
    let account: Account?
    let sources: [String]?
    let onlineOnly: Bool?
    let requireLogin: Bool?
    let allowRegistration: Bool?
}

struct Account: Codable, Equatable {
    let username: String?
    let admin: Bool?
    let onlineOnly: Bool?
    let sources: [String]?
    let createdAt: String?
}

struct LoginResponse: Codable {
    let ok: Bool?
    let account: Account?
}

struct Drama: Codable, Identifiable, Hashable {
    let id: String
    let source: String
    let title: String
    let cover: String?
    let desc: String?
    let totalEpisode: Int?
    let category: String?
    let releaseStatus: String?
    let vip: Bool?
    let score: String?
    let views: String?
    let tags: [String]?
    let onlineDate: String?

    var isVip: Bool { vip ?? false }
    var episodeCount: Int { totalEpisode ?? 0 }

    /// 站源归组：huangguo* / cloudfront → huangguo（对齐 Android 前缀匹配）
    var sourceGroup: String {
        let s = (source ?? "").lowercased()
        if s.hasPrefix("hongguo") { return "hongguo" }
        if s.hasPrefix("huangguo") || s == "cloudfront" { return "huangguo" }
        if s.hasPrefix("huangdou") { return "huangdou" }
        return s
    }

    var sourceLabel: String {
        switch sourceGroup {
        case "hongguo": return "红果"
        case "huangguo": return "黄果"
        case "huangdou": return "黄豆"
        default: return source ?? "未知"
        }
    }
}

/// 剧库响应：兼容 dramas/data、more/hasMore
struct DramasResponse: Codable {
    let dramas: [Drama]?
    let data: [Drama]?
    let loadedAt: String?
    let revision: Int?
    let more: Bool?
    let hasMore: Bool?
    let loading: Bool?
    var allDramas: [Drama] { dramas ?? data ?? [] }
    var morePages: Bool { more ?? hasMore ?? false }
}

struct SearchResponse: Codable {
    let query: String?
    let data: [Drama]?
    let total: Int?
}

struct RecommendationsResponse: Codable {
    let dramas: [Drama]?
}

struct Episode: Codable, Identifiable, Hashable {
    var id: Int { episode ?? index ?? 0 }
    let vip: Bool?
    let index: Int?
    let episode: Int?
    let title: String?
    let taskId: String?
    let chapterId: String?
    let number: Int?
    let total: Int?

    var epIndex: Int { episode ?? index ?? number ?? 1 }
    var displayTitle: String {
        title ?? String(format: "第%02d集", epIndex)
    }
}

struct PlaybackOpenResponse: Codable {
    let session: String?
    let dramaId: String?
    let title: String?
    let releaseStatus: String?
    let episodes: [Episode]?
    let mimeType: String?
    let mode: String?
    let initialIndex: Int?
    let initialPosition: Double?
    let resumePaused: Bool?
    let resumeMessage: String?
}

struct HlsOpenResponse: Codable {
    let url: String?
    let run: Int?
    let duration: Double?
    let source: String?
    let quality: Int?
    let qualities: [Quality]?
    let prefetched: Bool?
}

struct Quality: Codable, Hashable {
    let value: Int?
    let label: String?
}

struct PlaybackProgress: Codable {
    let run: Int
    let sequence: Int
    let episode: Int
    let position: Double
    let duration: Double
    let completed: Bool?
}

struct ControlBody: Codable {
    let session: String
    let action: String
    let progress: PlaybackProgress?
}

struct HeartbeatResponse: Codable {
    let state: String?
    let error: String?
    let run: Int?
    let duration: Double?
    let prefetch: String?
}

struct HistoryItem: Codable, Identifiable, Hashable {
    var id: String { dramaId + "#\(episode)" }
    let dramaId: String
    let source: String?
    let title: String?
    let chapterId: String?
    let episode: Int
    let index: Int?
    let total: Int?
    let position: Double?
    let duration: Double?
    let completed: Bool?
    let mode: String?
    let watchedAt: Double?
}

struct HistoryResponse: Codable {
    let data: [HistoryItem]?
    let limit: Int?
}

struct FollowingItem: Codable, Identifiable, Hashable {
    var id: String { dramaId }
    let dramaId: String
    let source: String?
    let title: String?
    let category: String?
    let knownEpisodes: Int?
    let totalEpisode: Int?
    let newEpisodes: Int?
    let saved: Bool?
    let completed: Bool?
    let addedAt: Double?
    let updatedAt: Double?
}

struct FollowingResponse: Codable {
    let data: [FollowingItem]?
    let limit: Int?
}

struct ApiErrorBody: Codable, Error {
    let error: String?
    let code: String?
}
// LocalizedError 扩展在 APIError.swift
