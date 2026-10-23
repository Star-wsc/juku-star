package com.juku.app.ui.components

fun sourceDisplayName(source: String?): String = when (source) {
    "hongguo" -> "红果"
    "huangguo" -> "黄果"
    "huangdou" -> "黄豆"
    "huangguo-video" -> "黄果视频"
    "cloudfront" -> "黄果原创"
    else -> source ?: ""
}
