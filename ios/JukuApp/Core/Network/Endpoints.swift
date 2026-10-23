import Foundation

/// 接口路径常量 — 对齐《API 速查表》
enum Endpoints {
    static let viewer = "/api/ui/viewer"
    static let login = "/api/ui/account/login"
    static let register = "/api/ui/account/register"
    static let logout = "/api/ui/account/logout"
    static let password = "/api/ui/account/password"

    static let dramas = "/api/ui/dramas"
    static let search = "/api/ui/search"
    static let searchSuggestions = "/api/ui/search/suggestions"
    static let rankings = "/api/ui/rankings"
    static let recommendations = "/api/ui/recommendations"

    static let following = "/api/ui/following"

    static let playbackOpen = "/api/ui/playback/open"
    static let playbackPlan = "/api/ui/playback/plan"
    static let playbackMediaOpen = "/api/ui/playback/media/open"
    static let hlsOpen = "/api/ui/playback/hls/open"
    static let hlsIndex = "/api/ui/playback/hls/index.m3u8"
    static let playbackControl = "/api/ui/playback/control"
    static let playbackProgress = "/api/ui/playback/progress"
    static let playbackPrefetch = "/api/ui/playback/prefetch"
    static let playbackHistory = "/api/ui/playback/history"
    static let playbackHistoryRemove = "/api/ui/playback/history/remove"

    static let image = "/api/ui/image"
}
