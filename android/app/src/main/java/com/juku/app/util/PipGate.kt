package com.juku.app.util

/**
 * 画中画门闩：Activity 在 onUserLeaveHint 时判断是否自动进小窗。
 * 由 PlayerScreen 挂载时登记，退出时清掉。
 */
object PipGate {
    @Volatile
    var playerActive: Boolean = false

    @Volatile
    var isPlaying: Boolean = false

    val shouldAutoEnter: Boolean get() = playerActive && isPlaying
}
