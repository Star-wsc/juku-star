package com.juku.app.domain.repository

import com.juku.app.domain.model.Drama
import com.juku.app.domain.model.DownloadTask
import com.juku.app.domain.model.FollowingItem
import com.juku.app.domain.model.HistoryItem
import com.juku.app.domain.model.HlsPlayback
import com.juku.app.domain.model.PlaybackSession
import com.juku.app.domain.model.RecommendationsPage
import com.juku.app.domain.model.Viewer
import kotlinx.coroutines.flow.Flow

/** 剧库分页结果 */
data class DramasPage(
    val dramas: List<Drama>,
    val revision: Long,
    val more: Boolean,
    /** 服务端剧库是否仍在后台从源站拉取 */
    val libraryLoading: Boolean = false
)

interface DramaRepository {
    suspend fun getDramas(refresh: Boolean = false): DramasPage
    suspend fun loadMore(revision: Long): DramasPage
    suspend fun refreshDrama(dramaId: String): Drama?
    suspend fun searchOnline(query: String): List<Drama>
    /** 2.0 搜索联想；1.0 或失败时返回空列表，不影响主搜索 */
    suspend fun searchSuggestions(query: String): List<String>
    suspend fun getRecommendations(
        genre: String,
        offset: Int = 0,
        sessionId: String = "",
        seen: List<String> = emptyList()
    ): RecommendationsPage
    fun cachedDramas(): Flow<List<Drama>>
    fun updateCache(dramas: List<Drama>)

    /** 切换服务器时调用：清空内存缓存与分页游标（revision/hasMore 属于旧服务器） */
    /** 仅重取服务端快照，不触发源站刷新 */
    suspend fun reloadSnapshot(): DramasPage

    /** 触发服务端从源站更新剧库；已更新中则返回 false */
    suspend fun updateLibrary(): Result<Boolean>

    fun resetCache()
}

interface AuthRepository {
    suspend fun bootstrap(): Result<Viewer>
    suspend fun login(username: String, password: String): Result<Unit>
    suspend fun register(username: String, password: String): Result<Unit>
    suspend fun logout(): Result<Unit>
    suspend fun changePassword(oldPassword: String, newPassword: String): Result<Unit>
    suspend fun testServer(url: String): Result<Viewer>
    val viewerState: Flow<Viewer?>
}

interface PlaybackRepository {
    /**
     * 打开播放会话。
     * @param fromHistory 仅在"从观看历史续播"入口为 true；服务端会校验对应历史记录存在，
     * 否则返回 404"此观看记录已移除"。从剧库/详情页进入必须为 false。
     */
    suspend fun open(dramaId: String, resume: Boolean = true, fromHistory: Boolean = false): Result<PlaybackSession>
    suspend fun openHls(
        session: String,
        episode: Int,
        start: Double? = null,
        quality: Int? = null
    ): Result<HlsPlayback>
    suspend fun heartbeat(session: String): Result<Unit>
    suspend fun close(session: String)
    suspend fun reportProgress(
        session: String,
        run: Long,
        sequence: Long,
        episode: Int,
        position: Double,
        duration: Double,
        completed: Boolean
    ): Result<Unit>
    suspend fun prefetch(session: String, episode: Int, run: Long, version: Long? = null): Result<Unit>

    /** 2.0/1.0 只读弹幕（30s 窗口）；失败返回空列表 */
    suspend fun fetchDanmaku(session: String, episode: Int, startMs: Long, durationMs: Long): List<com.juku.app.domain.model.DanmakuItem>

    /** 预准备下一集，减轻换集卡顿 */
    suspend fun prepareNext(session: String, episode: Int): Result<Unit>
}

interface FollowingRepository {
    fun observeFollowing(): Flow<List<FollowingItem>>
    suspend fun refresh(): Result<Unit>
    suspend fun follow(dramaId: String): Result<Unit>        // 想看（收藏）
    suspend fun markWatched(dramaId: String): Result<Unit>   // 已看
    suspend fun unfollow(dramaId: String): Result<Unit>
    suspend fun acknowledgeNew(dramaId: String): Result<Unit>

    /** 切换服务器时调用：清空本地追剧缓存（旧服务器数据不应残留） */
    suspend fun clearLocalCache()
}

interface HistoryRepository {
    fun observeHistory(): Flow<List<HistoryItem>>
    suspend fun refreshFromServer(): Result<Unit>
    suspend fun saveLocal(
        dramaId: String,
        source: String?,
        title: String,
        episode: Int,
        index: Int,
        total: Int,
        position: Double,
        duration: Double,
        completed: Boolean
    )
    suspend fun remove(dramaId: String): Result<Unit>
    suspend fun removeAll(): Result<Unit>

    /** 切换服务器时调用：清空本地历史缓存（旧服务器数据不应残留） */
    suspend fun clearLocalCache()
}

interface DownloadRepository {
    fun observeTasks(): Flow<List<DownloadTask>>
    suspend fun enqueue(dramaId: String, dramaTitle: String, source: String?, episodes: List<Pair<Int, String>>, quality: Int)
    suspend fun pause(taskId: Long)
    suspend fun resume(taskId: Long)
    suspend fun cancel(taskId: Long)
    suspend fun retry(taskId: Long)
    suspend fun removeRecord(taskId: Long, deleteFile: Boolean)
    suspend fun removeDrama(dramaId: String, deleteFiles: Boolean)
    suspend fun pauseDrama(dramaId: String)
    suspend fun resumeDrama(dramaId: String)
    suspend fun storageUsedBytes(): Long
    suspend fun downloadedEpisodes(dramaId: String): List<DownloadTask>
    suspend fun tasksForDrama(dramaId: String): List<DownloadTask>
    suspend fun getTask(taskId: Long): DownloadTask?
    suspend fun totalPendingCount(): Int
    fun ensureWorkerEnqueued()
}
