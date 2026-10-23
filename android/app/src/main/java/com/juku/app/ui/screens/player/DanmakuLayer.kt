package com.juku.app.ui.screens.player

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.offset
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import com.juku.app.domain.model.DanmakuItem
import kotlin.math.roundToInt

/** 一条弹幕从右缘飞到左缘的基准时间 */
private const val FLIGHT_MS = 8000L

/**
 * 只读弹幕层。
 * - withFrameNanos 逐帧位移，避免进度轮询量化成「一个字一个字蹦」
 * - 整条完全飘出屏幕左侧才卸载
 * - 暂停停住、跳转对齐、倍速跟随
 */
@Composable
fun DanmakuLayer(
    items: List<DanmakuItem>,
    positionMs: Long,
    isPlaying: Boolean,
    speed: Float = 1f,
    modifier: Modifier = Modifier
) {
    if (items.isEmpty()) return

    var framePosMs by remember { mutableLongStateOf(positionMs) }

    LaunchedEffect(isPlaying, speed) {
        if (!isPlaying) {
            framePosMs = positionMs
            return@LaunchedEffect
        }
        var lastNanos = 0L
        while (true) {
            withFrameNanos { now ->
                if (lastNanos == 0L) lastNanos = now
                val dtMs = (now - lastNanos) / 1_000_000.0
                lastNanos = now
                framePosMs += (dtMs * speed.coerceIn(0.25f, 4f)).toLong()
            }
        }
    }

    LaunchedEffect(positionMs) {
        if (!isPlaying || kotlin.math.abs(framePosMs - positionMs) > 2000L) {
            framePosMs = positionMs
        }
    }

    var viewportWidthPx by remember { mutableIntStateOf(0) }
    val density = androidx.compose.ui.platform.LocalDensity.current

    // 可见直到「飘出左侧」：flight 结束后再多留 35% 时间，保证最后一个字离屏
    val exitAfterMs = (FLIGHT_MS * 1.35f).toLong()
    val visible = items.filter { item ->
        framePosMs >= item.timeMs && framePosMs <= item.timeMs + exitAfterMs
    }.sortedBy { it.timeMs }.take(10)

    Box(
        modifier = modifier
            .fillMaxSize()
            .onSizeChanged { viewportWidthPx = it.width }
    ) {
        if (viewportWidthPx <= 0) return@Box
        val widthPx = viewportWidthPx.toFloat()

        visible.forEach { item ->
            val ageMs = (framePosMs - item.timeMs).toFloat()
            // progress 0 = 右缘进入；1 = 走完一屏；>1 继续向左飘出至完全离屏
            val progress = ageMs / FLIGHT_MS
            val x = widthPx * (1.1f - progress * 1.55f)

            val lane = (item.id.hashCode() and 0x7fffffff) % 5
            val yOffsetPx = with(density) { ((8 + lane * 28).dp).toPx() }

            Text(
                text = item.text,
                color = Color.White.copy(alpha = 0.92f),
                style = MaterialTheme.typography.bodyMedium.copy(
                    shadow = Shadow(
                        color = Color.Black.copy(alpha = 0.85f),
                        offset = Offset(1f, 1f),
                        blurRadius = 4f
                    )
                ),
                maxLines = 1,
                modifier = Modifier
                    .align(Alignment.TopStart)
                    .offset { IntOffset(x.roundToInt(), yOffsetPx.roundToInt()) }
            )
        }
    }
}
