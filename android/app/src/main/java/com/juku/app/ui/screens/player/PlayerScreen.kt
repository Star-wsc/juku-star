package com.juku.app.ui.screens.player

import android.app.Activity
import android.content.Context
import android.content.pm.ActivityInfo
import android.media.AudioManager
import android.os.Build
import android.view.WindowManager
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.activity.compose.BackHandler
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.material.icons.filled.ScreenRotation
import androidx.compose.material.icons.filled.PictureInPictureAlt
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.media3.ui.PlayerView
import com.juku.app.MainActivity
import com.juku.app.ui.components.LoadingState
import com.juku.app.util.PipGate
import com.juku.app.util.formatDurationSeconds
import kotlinx.coroutines.delay
import kotlin.math.abs
import kotlin.math.roundToInt

private val speedOptions = listOf(0.5f, 0.75f, 1.0f, 1.25f, 1.5f, 2.0f, 3.0f)

@Composable
fun PlayerScreen(
    onBack: () -> Unit,
    viewModel: PlayerViewModel = hiltViewModel()
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val snackbarHostState = remember { SnackbarHostState() }

    var showEpisodeDrawer by remember { mutableStateOf(false) }
    var gestureHint by remember { mutableStateOf<String?>(null) }

    val exitPlayer = {
        restoreSystemBarsForExit(context.findActivity())
        onBack()
    }
    BackHandler { exitPlayer() }

    // 快进/快退提示 1.2s 后自动消失，避免卡在屏幕上
    LaunchedEffect(gestureHint) {
        if (gestureHint != null) {
            delay(1200)
            gestureHint = null
        }
    }

    // 全屏沉浸 + 横竖屏自由旋转
    DisposableEffect(Unit) {
        val activity = context.findActivity()
        activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_FULL_SENSOR
        val window = activity?.window
        window?.decorView?.setBackgroundColor(android.graphics.Color.BLACK)
        window?.navigationBarColor = android.graphics.Color.BLACK
        window?.statusBarColor = android.graphics.Color.BLACK
        val controller = window?.let { WindowCompat.getInsetsController(it, it.decorView) }
        controller?.systemBarsBehavior =
            WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        controller?.hide(WindowInsetsCompat.Type.systemBars())
        onDispose {
            viewModel.persistProgress()
            restoreSystemBarsForExit(activity)
        }
    }

    // 控件 5 秒自动隐藏
    LaunchedEffect(state.controlsVisible, state.isPlaying) {
        if (state.controlsVisible && state.isPlaying) {
            delay(5000)
            viewModel.hideControls()
        }
    }

    LaunchedEffect(state.resumeMessage) {
        // 没看过的剧服务端可能返回空串，空白消息不弹 Snackbar（否则出现白条）
        state.resumeMessage?.let { msg ->
            if (msg.isNotBlank()) snackbarHostState.showSnackbar(msg)
            viewModel.consumeResumeMessage()
        }
    }

    // 2.0 兼容/解码提示：展示一次，不阻断播放
    LaunchedEffect(state.compatNotice) {
        state.compatNotice?.takeIf { it.isNotBlank() }?.let { msg ->
            snackbarHostState.showSnackbar(msg)
            viewModel.consumeCompatNotice()
        }
    }

    LaunchedEffect(state.error) {
        state.error?.takeIf { it.isNotBlank() }?.let {
            snackbarHostState.showSnackbar(it)
            viewModel.dismissError()
        }
    }

    val audioManager = remember { context.getSystemService(Context.AUDIO_SERVICE) as AudioManager }
    val maxVolume = remember { audioManager.getStreamMaxVolume(AudioManager.STREAM_MUSIC) }

    // 画中画门闩：挂载中才允许 Home 自动小窗
    DisposableEffect(state.isPlaying) {
        PipGate.playerActive = true
        PipGate.isPlaying = state.isPlaying
        onDispose {
            PipGate.playerActive = false
            PipGate.isPlaying = false
        }
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Color.Black)
    ) {
        // 视频层
        AndroidView(
            factory = { ctx ->
                PlayerView(ctx).apply {
                    useController = false
                    keepScreenOn = true
                }
            },
            // 仅在 player 实例变化时重新挂载，避免周期性重组反复触碰 Surface
            update = { view ->
                if (view.player !== viewModel.player) view.player = viewModel.player
            },
            modifier = Modifier.fillMaxSize()
        )

        // 缓冲指示
        if (state.isBuffering && state.error == null) {
            CircularProgressIndicator(
                modifier = Modifier
                    .size(44.dp)
                    .align(Alignment.Center),
                color = Color.White
            )
        }

        // 只读弹幕（逐帧动画）
        if (state.danmakuEnabled && state.danmakuItems.isNotEmpty()) {
            DanmakuLayer(
                items = state.danmakuItems,
                positionMs = state.positionMs,
                isPlaying = state.isPlaying,
                speed = state.speed,
                modifier = Modifier.fillMaxSize()
            )
        }

        // 手势：单击显隐控件；双击三分区 = 快退/播放暂停/快进；拖动调进度/亮度/音量
        var dragSeekPreviewMs by remember { mutableFloatStateOf(0f) }

        Box(
            modifier = Modifier
                .fillMaxSize()
                .pointerInput(Unit) {
                    val doubleTapWindowMs = 280L
                    val slop = viewConfiguration.touchSlop
                    var lastTapMs = 0L
                    var lastTapX = 0f
                    awaitPointerEventScope {
                        while (true) {
                            val downEvent = awaitPointerEvent()
                            val down = downEvent.changes.firstOrNull { it.pressed } ?: continue
                            val downX = down.position.x
                            val downPos = down.position
                            var moved = false
                            do {
                                val ev = awaitPointerEvent()
                                val ch = ev.changes.firstOrNull() ?: break
                                if ((ch.position - downPos).getDistance() > slop) moved = true
                            } while (ev.changes.any { it.pressed })

                            if (moved) continue

                            val now = System.currentTimeMillis()
                            val isDouble = (now - lastTapMs) <= doubleTapWindowMs &&
                                kotlin.math.abs(downX - lastTapX) < 80f
                            val width = size.width
                            val zone = when {
                                downX < width / 3f -> -1
                                downX > width * 2f / 3f -> 1
                                else -> 0
                            }
                            if (isDouble) {
                                lastTapMs = 0L
                                when (zone) {
                                    -1 -> {
                                        viewModel.seekBy(-10_000L)
                                        gestureHint = "快退 10 秒"
                                    }
                                    1 -> {
                                        viewModel.seekBy(10_000L)
                                        gestureHint = "快进 10 秒"
                                    }
                                    else -> {
                                        viewModel.togglePlay()
                                        gestureHint = null
                                    }
                                }
                                viewModel.showControls()
                            } else {
                                lastTapMs = now
                                lastTapX = downX
                                // 单击只切换控制栏，不快进/快退
                                viewModel.toggleControls()
                            }
                        }
                    }
                }
                .pointerInput(Unit) {
                    // 长按 3 倍速，松手恢复（与点击手势分离，避免互相干扰）
                    awaitPointerEventScope {
                        while (true) {
                            val down = awaitPointerEvent()
                                .changes.firstOrNull { it.pressed } ?: continue
                            val start = System.currentTimeMillis()
                            var longPressed = false
                            while (true) {
                                val ev = awaitPointerEvent()
                                if (ev.changes.none { it.pressed }) break
                                if (!longPressed && System.currentTimeMillis() - start >= 350) {
                                    longPressed = true
                                    viewModel.setSpeedWhilePressed(true)
                                }
                            }
                            if (longPressed) viewModel.setSpeedWhilePressed(false)
                        }
                    }
                }
                .pointerInput(maxVolume) {
                    var mode = 0 // 0 无, 1 横向 seek, 2 亮度, 3 音量
                    var startBrightness = 0.5f
                    var startVolume = 0
                    var accumX = 0f
                    var accumY = 0f
                    var seekPreview = 0f

                    fun readBrightness(): Float {
                        val w = context.findActivity()?.window ?: return 0.5f
                        val attr = w.attributes.screenBrightness
                        if (attr >= 0f) return attr.coerceIn(0.01f, 1f)
                        return try {
                            android.provider.Settings.System.getInt(
                                w.context.contentResolver,
                                android.provider.Settings.System.SCREEN_BRIGHTNESS,
                                128
                            ) / 255f
                        } catch (_: Exception) {
                            0.5f
                        }.coerceIn(0.01f, 1f)
                    }

                    detectDragGestures(
                        onDragStart = {
                            mode = 0
                            accumX = 0f
                            accumY = 0f
                            seekPreview = 0f
                            startBrightness = readBrightness()
                            startVolume = audioManager.getStreamVolume(AudioManager.STREAM_MUSIC)
                            viewModel.showControls()
                        },
                        onDragEnd = {
                            if (mode == 1 && abs(seekPreview) > 1f) {
                                viewModel.seekBy(seekPreview.toLong())
                            }
                            mode = 0
                            gestureHint = null
                            dragSeekPreviewMs = 0f
                        },
                        onDragCancel = {
                            mode = 0
                            gestureHint = null
                            dragSeekPreviewMs = 0f
                        }
                    ) { change, amount ->
                        change.consume()
                        if (mode == 0) {
                            accumX += amount.x
                            accumY += amount.y
                            if (abs(accumX) < 2f && abs(accumY) < 2f) return@detectDragGestures
                            mode = if (abs(accumX) > abs(accumY)) 1
                            else if (change.position.x < size.width / 2f) 2 else 3
                            if (mode == 1) {
                                seekPreview = accumX * 120f
                            } else {
                                accumY = 0f
                            }
                        }
                        when (mode) {
                            1 -> {
                                seekPreview += amount.x * 120f
                                dragSeekPreviewMs = seekPreview
                                gestureHint = if (seekPreview >= 0) "快进 ${abs((seekPreview / 1000).roundToInt())} 秒"
                                else "快退 ${abs((seekPreview / 1000).roundToInt())} 秒"
                            }
                            2 -> {
                                accumY += amount.y
                                val activity = context.findActivity()
                                val window = activity?.window ?: return@detectDragGestures
                                val newBrightness =
                                    (startBrightness - accumY / size.height * 1.5f).coerceIn(0.01f, 1f)
                                val attrs = window.attributes
                                attrs.screenBrightness = newBrightness
                                window.attributes = attrs
                                gestureHint = "亮度 ${(newBrightness * 100).roundToInt()}%"
                            }
                            3 -> {
                                accumY += amount.y
                                val newVolume = (startVolume + (-accumY / size.height * maxVolume * 1.5f).roundToInt())
                                    .coerceIn(0, maxVolume)
                                audioManager.setStreamVolume(AudioManager.STREAM_MUSIC, newVolume, 0)
                                gestureHint = "音量 $newVolume/$maxVolume"
                            }
                        }
                    }
                }
        )

        // 手势提示
        gestureHint?.let { hint ->
            Text(
                hint,
                color = Color.White,
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier
                    .align(Alignment.Center)
                    .background(
                        Color.Black.copy(alpha = 0.5f),
                        MaterialTheme.shapes.small
                    )
                    .padding(horizontal = 16.dp, vertical = 8.dp)
            )
        }

        // 快进预览覆盖在进度条上
        if (dragSeekPreviewMs != 0f) {
            Text(
                "${formatDurationSeconds(((state.positionMs + dragSeekPreviewMs) / 1000.0).coerceAtLeast(0.0))} / ${formatDurationSeconds(state.durationMs / 1000.0)}",
                color = Color.White,
                modifier = Modifier.align(Alignment.BottomCenter).padding(bottom = 120.dp)
            )
        }

        // 控制层（独立子树 + 淡入淡出）
        AnimatedVisibility(
            visible = state.controlsVisible,
            enter = fadeIn(animationSpec = tween(250)),
            exit = fadeOut(animationSpec = tween(350))
        ) {
            PlayerControlsOverlay(
                state = state,
                viewModel = viewModel,
                onBack = onBack,
                onShowEpisodeDrawer = { showEpisodeDrawer = true }
            )
        }

        // 加载中 / 错误兜底（背景全屏纯黑，内容 safeDrawing 避让）
        if (state.loading) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(Color.Black),
                contentAlignment = Alignment.Center
            ) {
                Box(Modifier.windowInsetsPadding(WindowInsets.safeDrawing)) {
                    LoadingState(message = "正在打开播放会话…")
                }
            }
        }

        if (state.error != null && state.episodes.isEmpty()) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(Color.Black),
                contentAlignment = Alignment.Center
            ) {
                Box(Modifier.windowInsetsPadding(WindowInsets.safeDrawing)) {
                    Button(onClick = exitPlayer) { Text("返回") }
                }
            }
        }

        SnackbarHost(
            hostState = snackbarHostState,
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .windowInsetsPadding(WindowInsets.safeDrawing)
        )
    }

    if (showEpisodeDrawer) {
        EpisodeDrawer(
            episodes = state.episodes,
            currentEpisode = state.currentEpisode,
            onSelect = viewModel::playEpisode,
            onDismiss = { showEpisodeDrawer = false }
        )
    }
}

/**
 * 播放器控制层。从 PlayerScreen 拆出的独立重组子树：
 * 进度条/时长文本每 500ms 随播放位置刷新，拆开后这些重组只发生在本函数内，
 * 视频层（AndroidView）与手势层不再跟着整屏重组，消除播放时的周期性卡顿。
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PlayerControlsOverlay(
    state: PlayerUiState,
    viewModel: PlayerViewModel,
    onBack: () -> Unit,
    onShowEpisodeDrawer: () -> Unit
) {
    var qualityMenuOpen by remember { mutableStateOf(false) }
    var speedMenuOpen by remember { mutableStateOf(false) }
    Column(
        Modifier
            .fillMaxSize()
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .background(Color.Transparent)
    ) {
        // 顶部栏
        Row(
            Modifier
                .fillMaxWidth()
                .background(Color.Black.copy(alpha = 0.35f))
                .padding(horizontal = 4.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            IconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回", tint = Color.White)
            }
            Column(Modifier.weight(1f)) {
                Text(
                    state.title,
                    color = Color.White,
                    style = MaterialTheme.typography.titleMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis
                )
                Text(
                    state.episodeTitle,
                    color = Color.White.copy(alpha = 0.7f),
                    style = MaterialTheme.typography.bodySmall,
                    maxLines = 1
                )
            }
            val activity = LocalContext.current.findActivity()
            val configuration = LocalConfiguration.current
            val isLandscape = configuration.orientation == android.content.res.Configuration.ORIENTATION_LANDSCAPE
            IconButton(onClick = {
                activity?.requestedOrientation = if (isLandscape) {
                    android.content.pm.ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT
                } else {
                    android.content.pm.ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                }
            }) {
                Icon(
                    Icons.Filled.ScreenRotation,
                    contentDescription = if (isLandscape) "竖屏" else "横屏",
                    tint = Color.White
                )
            }
            IconButton(onClick = {
                (activity as? MainActivity)?.enterPictureInPictureCompat()
            }) {
                Icon(
                    Icons.Filled.PictureInPictureAlt,
                    contentDescription = "小窗播放",
                    tint = Color.White
                )
            }
        }

        Spacer(Modifier.weight(1f))

        // 底部控制栏
        Column(
            Modifier
                .fillMaxWidth()
                .background(Color.Black.copy(alpha = 0.35f))
                .padding(horizontal = 12.dp, vertical = 8.dp)
        ) {
            var sliderDragging by remember { mutableStateOf(false) }
            var sliderValue by remember { mutableFloatStateOf(0f) }
            val durationMsSafe = state.durationMs.coerceAtLeast(1L).toFloat()
            val playedFraction =
                ((if (sliderDragging) sliderValue else state.positionMs.toFloat()) / durationMsSafe)
                    .coerceIn(0f, 1f)
            val bufferedFraction = (state.bufferedMs.toFloat() / durationMsSafe).coerceIn(0f, 1f)
            val trackHeight by animateDpAsState(if (sliderDragging) 5.dp else 3.dp, label = "trackHeight")
            val thumbSize by animateDpAsState(if (sliderDragging) 15.dp else 9.dp, label = "thumbSize")

            // 细轨道进度条：白色已播 + 浅色已缓冲 + 半透明余量，两端时间标注，
            // 拖动时轨道变粗、拇指放大（配合 45-90 秒前向缓冲，余量一眼可见）
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth()
            ) {
                Text(
                    formatDurationSeconds(
                        (if (sliderDragging) sliderValue else state.positionMs.toFloat()) / 1000.0
                    ),
                    color = Color.White.copy(alpha = 0.8f),
                    style = MaterialTheme.typography.labelSmall
                )
                Slider(
                    value = if (sliderDragging) sliderValue
                    else state.positionMs.toFloat().coerceAtMost(durationMsSafe),
                    onValueChange = {
                        sliderDragging = true
                        sliderValue = it
                        viewModel.showControls()
                    },
                    onValueChangeFinished = {
                        viewModel.seekTo(sliderValue.toLong())
                        sliderDragging = false
                    },
                    valueRange = 0f..durationMsSafe,
                    thumb = {
                        Box(
                            Modifier
                                .size(thumbSize)
                                .background(Color.White, CircleShape)
                        )
                    },
                    track = {
                        Box(
                            Modifier
                                .fillMaxWidth()
                                .height(trackHeight)
                                .clip(CircleShape)
                                .background(Color.White.copy(alpha = 0.20f))
                        ) {
                            Box(
                                Modifier
                                    .fillMaxHeight()
                                    .fillMaxWidth(bufferedFraction)
                                    .background(Color.White.copy(alpha = 0.35f))
                            )
                            Box(
                                Modifier
                                    .fillMaxHeight()
                                    .fillMaxWidth(playedFraction)
                                    .background(Color.White)
                            )
                        }
                    },
                    modifier = Modifier.weight(1f)
                )
                Text(
                    formatDurationSeconds(state.durationMs / 1000.0),
                    color = Color.White.copy(alpha = 0.8f),
                    style = MaterialTheme.typography.labelSmall
                )
            }
            Row(
                Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically
            ) {
                IconButton(onClick = viewModel::playPrevious, enabled = state.currentEpisode > 1) {
                    Icon(Icons.Filled.SkipPrevious, "上一集", tint = Color.White)
                }
                IconButton(onClick = viewModel::togglePlay) {
                    Icon(
                        if (state.isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                        "播放/暂停",
                        tint = Color.White
                    )
                }
                IconButton(
                    onClick = viewModel::playNext,
                    enabled = state.episodes.any { it.episode == state.currentEpisode + 1 }
                ) {
                    Icon(Icons.Filled.SkipNext, "下一集", tint = Color.White)
                }
                Spacer(Modifier.weight(1f))
                // 弹幕开关：底部大按钮，开启高亮
                val dmOn = state.danmakuEnabled
                TextButton(
                    onClick = {
                        android.util.Log.d("JukuDanmaku", "button click")
                        viewModel.toggleDanmaku()
                    },
                    modifier = Modifier
                        .clip(CircleShape)
                        .background(
                            if (dmOn) Color(0xFF6C5CE7).copy(alpha = 0.85f)
                            else Color.White.copy(alpha = 0.12f)
                        )
                ) {
                    Text(
                        if (dmOn) "弹 开" else "弹 关",
                        color = if (dmOn) Color.White else Color.White.copy(alpha = 0.75f)
                    )
                }
                TextButton(onClick = onShowEpisodeDrawer) {
                    Text("选集", color = Color.White)
                }
                Box {
                    TextButton(onClick = { qualityMenuOpen = true }) {
                        Text(
                            state.qualities.find { it.value == state.currentQuality }?.label
                                ?: "画质",
                            color = Color.White
                        )
                    }
                    DropdownMenu(
                        expanded = qualityMenuOpen,
                        onDismissRequest = { qualityMenuOpen = false }
                    ) {
                        state.qualities.forEach { q ->
                            DropdownMenuItem(
                                text = { Text(q.label) },
                                onClick = {
                                    qualityMenuOpen = false
                                    viewModel.setQuality(q.value)
                                }
                            )
                        }
                    }
                }
                Box {
                    TextButton(onClick = { speedMenuOpen = true }) {
                        Text("${state.speed}x", color = Color.White)
                    }
                    DropdownMenu(
                        expanded = speedMenuOpen,
                        onDismissRequest = { speedMenuOpen = false }
                    ) {
                        speedOptions.forEach { s ->
                            DropdownMenuItem(
                                text = { Text("${s}x") },
                                onClick = {
                                    speedMenuOpen = false
                                    viewModel.setSpeed(s)
                                }
                            )
                        }
                    }
                }
            }
        }
    }
}


/** 退出播放：立刻显示系统栏并请求重新分发 insets，避免详情页顶到状态栏下。 */
private fun restoreSystemBarsForExit(activity: Activity?) {
    val window = activity?.window ?: return
    val controller = WindowCompat.getInsetsController(window, window.decorView)
    controller.show(WindowInsetsCompat.Type.systemBars())
    controller.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_DEFAULT
    window.statusBarColor = android.graphics.Color.TRANSPARENT
    window.navigationBarColor = android.graphics.Color.TRANSPARENT
    window.decorView.setBackgroundColor(android.graphics.Color.TRANSPARENT)
    ViewCompat.requestApplyInsets(window.decorView)
    activity.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
}

private fun Context.findActivity(): Activity? {
    var current = this
    while (current is android.content.ContextWrapper) {
        if (current is Activity) return current
        current = current.baseContext
    }
    return null
}
