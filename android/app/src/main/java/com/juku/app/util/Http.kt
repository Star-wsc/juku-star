package com.juku.app.util

import com.google.gson.Gson
import com.juku.app.data.remote.models.ApiErrorBody
import retrofit2.HttpException
import java.io.IOException
import java.util.Locale

/** 把服务端返回的相对路径（/api/ui/image?url=...）解析为绝对 URL */
fun resolveUrl(baseUrl: String, pathOrUrl: String?): String {
    if (pathOrUrl.isNullOrBlank()) return ""
    if (pathOrUrl.startsWith("http://") || pathOrUrl.startsWith("https://")) return pathOrUrl
    val base = baseUrl.trimEnd('/')
    return if (pathOrUrl.startsWith("/")) base + pathOrUrl else "$base/$pathOrUrl"
}

/** URL 去掉 query/fragment 后的文件名小写，用于识别媒体类型 */
private fun mediaPathOf(url: String): String {
    val noFrag = url.substringBefore('#')
    val noQuery = noFrag.substringBefore('?')
    return noQuery.substringAfterLast('/').lowercase(Locale.ROOT)
}

/**
 * 是否 HLS 播放列表。后端开启「优先原始 MP4 / HLS」后可能下发 CDN 上的
 * 原始 m3u8 或 mp4，不能再默认按 HLS 处理。
 */
fun isHlsMedia(url: String, serverMime: String? = null): Boolean {
    val mime = serverMime?.trim()?.lowercase(Locale.ROOT).orEmpty()
    if (mime.contains("mpegurl") || mime.contains("m3u8")) return true
    if (mime.contains("mp4")) return false
    val path = mediaPathOf(url)
    if (path.endsWith(".m3u8") || path.endsWith(".m3u")) return true
    if (path.endsWith(".mp4") || path.endsWith(".m4v") || path.endsWith(".mov")) return false
    // 服务端代理 HLS 路径：/api/ui/playback/hls/index.m3u8?...
    if (url.contains("/playback/hls/", ignoreCase = true)) return true
    // 默认按 HLS（1.0 代理链路）
    return true
}

/**
 * 给 ExoPlayer 用的 MIME。
 * 后端优先原始播放时绝不能写死 APPLICATION_M3U8；但未知类型时
 * 仍回落 m3u8，保持与 1.3.x 代理 HLS 链路旧行为一致，避免回归。
 */
fun mediaMimeType(url: String, serverMime: String? = null): String {
    val mime = serverMime?.trim()?.lowercase(Locale.ROOT).orEmpty()
    if (mime.isNotBlank()) {
        return when {
            mime.contains("mpegurl") || mime.contains("m3u8") -> "application/x-mpegURL"
            mime.contains("mp4") -> "video/mp4"
            else -> mime
        }
    }
    val path = mediaPathOf(url)
    return when {
        path.endsWith(".m3u8") || path.endsWith(".m3u") -> "application/x-mpegURL"
        path.endsWith(".mp4") || path.endsWith(".m4v") -> "video/mp4"
        path.endsWith(".mov") -> "video/quicktime"
        // 未知：按 HLS（与写死 APPLICATION_M3U8 的旧版一致）
        else -> "application/x-mpegURL"
    }
}

/** 是否原始/直链媒体文件（mp4 等，非 HLS 列表）——下载时走整文件下载 */
fun isDirectMediaUrl(url: String, serverMime: String? = null): Boolean = !isHlsMedia(url, serverMime)

/** 统一把异常翻译成用户可读的中文提示 */
fun Throwable.apiMessage(): String = when (this) {
    is HttpException -> {
        val body = runCatching { response()?.errorBody()?.string() }.getOrNull()
        val parsed = body?.let {
            runCatching { Gson().fromJson(it, ApiErrorBody::class.java) }.getOrNull()
        }
        parsed?.error ?: parsed?.message ?: "服务器错误 (${code()})"
    }
    is IOException -> "网络连接失败，请检查服务器地址与网络"
    else -> message ?: localizedMessage ?: "未知错误"
}

/** "100万" / "1.2亿" / "3456" -> Long，用于排序 */
fun parseViewCount(raw: String?): Long {
    if (raw.isNullOrBlank()) return 0L
    val cleaned = raw.trim().lowercase(Locale.ROOT)
    val numberPart = cleaned.takeWhile { it.isDigit() || it == '.' }
    val number = numberPart.toDoubleOrNull() ?: return 0L
    return when {
        cleaned.contains("亿") -> (number * 100_000_000).toLong()
        cleaned.contains("万") || cleaned.contains("w") -> (number * 10_000).toLong()
        else -> number.toLong()
    }
}

fun formatDurationSeconds(seconds: Double): String {
    val total = seconds.toLong().coerceAtLeast(0)
    val h = total / 3600
    val m = (total % 3600) / 60
    val s = total % 60
    return if (h > 0) String.format(Locale.US, "%d:%02d:%02d", h, m, s)
    else String.format(Locale.US, "%02d:%02d", m, s)
}

fun formatBytes(bytes: Long): String {
    if (bytes <= 0) return "0 B"
    val units = arrayOf("B", "KB", "MB", "GB", "TB")
    var value = bytes.toDouble()
    var index = 0
    while (value >= 1024 && index < units.size - 1) {
        value /= 1024
        index++
    }
    return if (index == 0) "${bytes} B"
    else String.format(Locale.US, "%.1f %s", value, units[index])
}
