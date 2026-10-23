package com.juku.app.domain.model

data class Drama(
    val id: String,
    val source: String,
    val title: String,
    val coverUrl: String,          // 已解析为绝对 URL
    val desc: String,
    val totalEpisode: Int,
    val category: String,
    val releaseStatus: String,     // finished | ongoing | unknown
    val vip: Boolean,
    val score: String,
    val views: String,
    val tags: List<String>,
    val onlineDate: String
)

/** 红果分类推荐的分类（与 WEB recommendGenre 一致） */
data class RecommendGenre(
    val id: String,
    val label: String
)

/** 一轮分类推荐结果 */
data class RecommendationsPage(
    val dramas: List<Drama>,
    val hasMore: Boolean,
    val nextOffset: Int,
    val sessionId: String
)

data class Viewer(
    val id: String?,
    val account: Account?,
    val sources: List<String>,
    val sourceNames: Map<String, String> = emptyMap(),
    val requireLogin: Boolean,
    val allowRegistration: Boolean
) {
    val isLoggedIn: Boolean get() = account != null
}

data class Account(
    val username: String,
    val admin: Boolean,
    val sources: List<String>
)

data class Episode(
    val index: Int,      // 1-based 播放顺序
    val episode: Int,    // 1-based 集号（hls/open 用）
    val title: String,
    val vip: Boolean,
    val hasDanmaku: Boolean = false
)

/** 只读弹幕条目 */
data class DanmakuItem(
    val id: String,
    val text: String,
    val timeMs: Long
)

data class PlaybackSession(
    val session: String,
    val dramaId: String,
    val title: String,
    val releaseStatus: String,
    val episodes: List<Episode>,
    val initialIndex: Int,
    val initialPosition: Double,
    val resumePaused: Boolean,
    val resumeMessage: String?
)

data class QualityOption(val value: Int, val label: String)

data class HlsPlayback(
    val url: String,               // 绝对 URL
    val run: Long,                 // 服务端返回的播放轮次（数值类型，上报时原样回传）
    val duration: Double,
    val quality: Int,
    val qualities: List<QualityOption>,
    val prefetched: Boolean,
    /** 2.0 兼容性/解码提示；1.0 通常为 null */
    val compatNotice: String? = null,
    /** 服务端下发的 MIME；后端「优先原始 MP4 / HLS」时可能是 video/mp4 */
    val mimeType: String? = null,
    /** 是否 HLS 列表（代理 m3u8 或原始 m3u8）；原始 MP4 等直链为 false */
    val isHls: Boolean = true
)

/** 搜索联想条目（2.0 search/suggestions） */
data class SearchSuggestion(
    val text: String,
    val dramaId: String? = null
)

data class FollowingItem(
    val dramaId: String,
    val source: String?,
    val title: String,
    val category: String?,
    val knownEpisodes: Int,
    val totalEpisode: Int,
    val newEpisodes: Int,
    val saved: Boolean,
    val completed: Boolean,
    val updatedAt: String?
)

data class HistoryItem(
    val dramaId: String,
    val source: String?,
    val title: String,
    val episode: Int,
    val index: Int,
    val total: Int,
    val position: Double,
    val duration: Double,
    val completed: Boolean,
    val watchedAt: Long
)

data class DownloadTask(
    val id: Long,
    val dramaId: String,
    val dramaTitle: String,
    val episode: Int,
    val title: String,
    val quality: Int,
    val status: String,
    val progress: Int,
    val downloadedBytes: Long,
    val totalBytes: Long,
    val speedBytesPerSecond: Long,
    val filePath: String?,
    val fileSize: Long,
    val error: String?
)

/** 追剧状态：未追 / 想看（无历史）/ 在看（有未看完历史）/ 已看 */
enum class FollowState { NONE, WANT, WATCHING, WATCHED }

/**
 * 历史是否“未看完”。
 * 注意 [HistoryItem.completed] 只表示当前集看完，不等于整部剧看完。
 */
fun HistoryItem.isUnfinished(): Boolean {
    if (!completed) return true
    if (total > 0 && index < total) return true
    if (duration > 0 && position < duration) return true
    return false
}
