package com.juku.app.ui.components

/** 红果版：界面只出现红果，不编译其他站源名称 */
fun sourceDisplayName(source: String?): String {
    return if (source != null && source.startsWith("hongguo")) "红果" else ""
}
