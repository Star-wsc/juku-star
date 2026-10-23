package com.juku.app.service

import android.content.Context
import androidx.hilt.work.HiltWorker
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.juku.app.data.local.db.DownloadTaskDao
import com.juku.app.data.local.db.DownloadTaskEntity
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.data.remote.JukuApi
import com.juku.app.data.remote.models.OpenPlaybackRequest
import com.juku.app.data.remote.models.HlsOpenRequest
import com.juku.app.domain.repository.PlaybackRepository
import com.juku.app.util.resolveUrl
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.withContext
import java.io.File

/**
 * 串行处理本地下载队列：
 * 循环取下一个 queued 任务 -> 打开播放会话 -> 获取播放地址 -> 下载（HLS 逐片 / 原始直链整文件）-> 合并保存。
 * 一次只下载一个分集，避免触发服务端 4 并发播放会话上限。
 */
@HiltWorker
class DownloadWorker @AssistedInject constructor(
    @Assisted appContext: Context,
    @Assisted params: WorkerParameters,
    private val api: JukuApi,
    private val dao: DownloadTaskDao,
    private val prefs: AppPrefs,
    private val downloader: HlsDownloader,
    private val playbackRepository: PlaybackRepository
) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result {
        try {
            while (!isStopped) {
                val task = dao.currentRunning() ?: dao.nextQueued() ?: break
                processTask(task)
            }
            return Result.success()
        } catch (e: CancellationException) {
            // 系统中断（约束丢失/进程停止）：把 running 任务放回队列，下次继续
            withContext(kotlinx.coroutines.NonCancellable) {
                runCatching {
                    dao.currentRunning()?.let {
                        if (it.status == DownloadTaskEntity.STATUS_RUNNING) {
                            dao.updateStatus(it.id, DownloadTaskEntity.STATUS_QUEUED, null)
                        }
                    }
                }
            }
            throw e
        }
    }

    private suspend fun processTask(task: DownloadTaskEntity) {
        if (task.status == DownloadTaskEntity.STATUS_SUCCESS) return
        if (task.status !in listOf(
                DownloadTaskEntity.STATUS_QUEUED,
                DownloadTaskEntity.STATUS_RUNNING
            )
        ) return

        dao.updateStatus(task.id, DownloadTaskEntity.STATUS_RUNNING, null)
        var session: String? = null
        try {
            val open = api.openPlayback(OpenPlaybackRequest(dramaId = task.dramaId, resume = false))
            session = open.session ?: error(open.error ?: "无法打开播放会话")
            val hls = com.juku.app.data.remote.PlaybackCompat.openHlsWithCompat(
                api,
                HlsOpenRequest(
                    session = session,
                    episode = task.episode,
                    quality = task.quality.takeIf { it != Int.MIN_VALUE }
                )
            )
            val playlistUrl = resolveUrl(prefs.serverUrl, hls.url ?: error(hls.error ?: "获取播放地址失败"))

            val targetFile = offlineFile(task.dramaId, task.episode, task.quality, playlistUrl)
            val tmpDir = File(applicationContext.filesDir, "downloads/tmp/${sanitize(task.dramaId)}_${task.episode}")

            var lastUpdate = 0L
            downloader.download(
                playlistUrl = playlistUrl,
                targetFile = targetFile,
                tmpDir = tmpDir,
                onProgress = { bytes, index, total ->
                    val now = System.currentTimeMillis()
                    if (now - lastUpdate > 600) {
                        lastUpdate = now
                        val current = dao.getById(task.id)
                        if (current != null) {
                            val elapsed = (now - current.updatedAt).coerceAtLeast(1)
                            val speed = ((bytes - current.downloadedBytes) * 1000 / elapsed).coerceAtLeast(0)
                            dao.update(
                                current.copy(
                                    progress = (index * 100 / total).coerceIn(0, 100),
                                    downloadedBytes = bytes,
                                    speedBytesPerSecond = speed,
                                    updatedAt = now
                                )
                            )
                        }
                    }
                },
                shouldAbort = {
                    val current = runCatching { dao.getById(task.id) }.getOrNull()
                    current == null || current.status != DownloadTaskEntity.STATUS_RUNNING
                }
            )

            val finished = dao.getById(task.id)
            if (finished != null && finished.status == DownloadTaskEntity.STATUS_RUNNING) {
                dao.update(
                    finished.copy(
                        status = DownloadTaskEntity.STATUS_SUCCESS,
                        progress = 100,
                        filePath = targetFile.absolutePath,
                        fileSize = targetFile.length(),
                        speedBytesPerSecond = 0,
                        error = null,
                        updatedAt = System.currentTimeMillis()
                    )
                )
            }
        } catch (e: DownloadAbortedException) {
            // 用户已暂停/取消，状态由操作方维护，不覆盖
        } catch (e: Exception) {
            val current = dao.getById(task.id)
            if (current != null && current.status == DownloadTaskEntity.STATUS_RUNNING) {
                dao.update(
                    current.copy(
                        status = DownloadTaskEntity.STATUS_FAILED,
                        error = e.message ?: "下载失败",
                        updatedAt = System.currentTimeMillis()
                    )
                )
            }
        } finally {
            session?.let { playbackRepository.close(it) }
        }
    }

    private fun offlineFile(dramaId: String, episode: Int, quality: Int, mediaUrl: String): File {
        val dir = File(applicationContext.filesDir, "downloads/${sanitize(dramaId)}")
        val q = if (quality == Int.MIN_VALUE) "def" else quality.toString()
        // 原始 MP4 直链存 .mp4，HLS 代理/列表拼成 .ts，播放端都可直接打开
        val ext = if (com.juku.app.util.isDirectMediaUrl(mediaUrl)) "mp4" else "ts"
        return File(dir, "ep_%03d_q%s.%s".format(episode, q, ext))
    }

    private fun sanitize(raw: String): String =
        raw.map { c -> if (c.isLetterOrDigit() || c == '-') c else '_' }.joinToString("")
}
