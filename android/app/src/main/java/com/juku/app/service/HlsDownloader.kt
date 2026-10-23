package com.juku.app.service

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton

/** 用户主动暂停/取消时抛出，worker 据此不标记为失败 */
class DownloadAbortedException : Exception("下载已中止")

/**
 * 极简 HLS VOD 下载器：
 * 1. 拉取 m3u8 文本，解析出全部分片 URL（支持相对路径）
 * 2. 逐片下载到临时目录（每片最多重试 3 次，片间检查中止标记）
 * 3. 顺序拼接为单个 MPEG-TS 文件（ExoPlayer 可直接播放）
 *
 * 另支持后端「优先原始 MP4 / HLS」下发的整文件直链（mp4 等）。
 */
@Singleton
class HlsDownloader @Inject constructor(private val client: OkHttpClient) {

    suspend fun download(
        playlistUrl: String,
        targetFile: File,
        tmpDir: File,
        onProgress: suspend (downloadedBytes: Long, segmentIndex: Int, segmentTotal: Int) -> Unit,
        shouldAbort: suspend () -> Boolean
    ) {
        if (com.juku.app.util.isDirectMediaUrl(playlistUrl)) {
            downloadDirect(playlistUrl, targetFile, onProgress, shouldAbort)
            return
        }
        tmpDir.mkdirs()
        targetFile.parentFile?.mkdirs()
        val segments = withContext(Dispatchers.IO) { fetchSegments(playlistUrl) }
        if (segments.isEmpty()) error("播放列表中没有分片")

        var downloaded = 0L
        try {
            for ((index, segmentUrl) in segments.withIndex()) {
                if (shouldAbort()) throw DownloadAbortedException()
                val segFile = File(tmpDir, "seg_%05d.ts".format(index))
                val bytes = withContext(Dispatchers.IO) {
                    downloadSegment(segmentUrl, segFile)
                }
                downloaded += bytes
                onProgress(downloaded, index + 1, segments.size)
            }
            if (shouldAbort()) throw DownloadAbortedException()
            withContext(Dispatchers.IO) { concat(targetFile, tmpDir, segments.size) }
        } finally {
            withContext(Dispatchers.IO) { tmpDir.deleteRecursively() }
        }
    }

    /** 原始 MP4 等直链：整文件下载 */
    private suspend fun downloadDirect(
        url: String,
        targetFile: File,
        onProgress: suspend (downloadedBytes: Long, segmentIndex: Int, segmentTotal: Int) -> Unit,
        shouldAbort: suspend () -> Boolean
    ) {
        targetFile.parentFile?.mkdirs()
        val part = File(targetFile.absolutePath + ".part")
        try {
            var downloaded = 0L
            var lastReport = 0L
            val request = Request.Builder().url(url).build()
            val response = withContext(Dispatchers.IO) { client.newCall(request).execute() }
            response.use { resp ->
                if (!resp.isSuccessful) throw IOException("HTTP ${resp.code}")
                val body = resp.body ?: throw IOException("空响应体")
                val input = body.byteStream()
                val out = part.outputStream()
                out.use {
                    val buffer = ByteArray(256 * 1024)
                    while (true) {
                        if (shouldAbort()) throw DownloadAbortedException()
                        val n = withContext(Dispatchers.IO) { input.read(buffer) }
                        if (n < 0) break
                        withContext(Dispatchers.IO) { out.write(buffer, 0, n) }
                        downloaded += n
                        if (downloaded - lastReport >= 512 * 1024) {
                            lastReport = downloaded
                            onProgress(downloaded, 1, 1)
                        }
                    }
                }
            }
            if (shouldAbort()) throw DownloadAbortedException()
            targetFile.delete()
            if (!part.renameTo(targetFile)) error("保存下载文件失败")
            onProgress(downloaded, 1, 1)
        } catch (e: Exception) {
            part.delete()
            throw e
        }
    }

    private fun fetchSegments(playlistUrl: String): List<String> {
        val request = Request.Builder().url(playlistUrl).build()
        client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) error("获取播放列表失败: HTTP ${response.code}")
            val body = response.body?.string() ?: error("播放列表为空")
            return body.lineSequence()
                .map { it.trim() }
                .filter { it.isNotEmpty() && !it.startsWith("#") }
                .map { resolveSegmentUrl(playlistUrl, it) }
                .toList()
        }
    }

    private fun resolveSegmentUrl(playlistUrl: String, segment: String): String {
        if (segment.startsWith("http://") || segment.startsWith("https://")) return segment
        return playlistUrl.toHttpUrlOrNull()?.resolve(segment)?.toString() ?: segment
    }

    private fun downloadSegment(url: String, target: File): Long {
        var lastError: Exception? = null
        repeat(3) { attempt ->
            try {
                val request = Request.Builder().url(url).build()
                client.newCall(request).execute().use { response ->
                    if (!response.isSuccessful) throw IOException("HTTP ${response.code}")
                    val body = response.body ?: throw IOException("空响应体")
                    target.outputStream().use { out -> body.byteStream().copyTo(out, 64 * 1024) }
                }
                return target.length()
            } catch (e: DownloadAbortedException) {
                throw e
            } catch (e: Exception) {
                lastError = e
                target.delete()
                try { Thread.sleep(500L * (attempt + 1)) } catch (_: InterruptedException) {}
            }
        }
        throw lastError ?: IOException("分片下载失败")
    }

    private fun concat(targetFile: File, tmpDir: File, segmentCount: Int) {
        val part = File(targetFile.absolutePath + ".part")
        part.outputStream().use { out ->
            for (i in 0 until segmentCount) {
                File(tmpDir, "seg_%05d.ts".format(i)).inputStream().use { it.copyTo(out, 256 * 1024) }
            }
        }
        if (!part.renameTo(targetFile)) {
            targetFile.delete()
            if (!part.renameTo(targetFile)) error("合并分片失败")
        }
    }
}
