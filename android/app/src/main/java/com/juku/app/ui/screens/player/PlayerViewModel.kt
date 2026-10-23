package com.juku.app.ui.screens.player

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.PlaybackParameters
import androidx.media3.common.Player
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.exoplayer.upstream.DefaultLoadErrorHandlingPolicy
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.domain.model.DownloadTask
import com.juku.app.domain.model.Episode
import com.juku.app.domain.model.HlsPlayback
import com.juku.app.domain.model.PlaybackSession
import com.juku.app.domain.model.QualityOption
import com.juku.app.domain.repository.DownloadRepository
import com.juku.app.domain.repository.HistoryRepository
import com.juku.app.domain.repository.PlaybackRepository
import com.juku.app.util.apiMessage
import com.juku.app.util.formatDurationSeconds
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.util.concurrent.atomic.AtomicLong
import javax.inject.Inject

data class PlayerUiState(
    val loading: Boolean = true,
    val error: String? = null,
    val title: String = "",
    val episodeTitle: String = "",
    val episodes: List<Episode> = emptyList(),
    val currentEpisode: Int = 0,
    val qualities: List<QualityOption> = emptyList(),
    val currentQuality: Int = Int.MIN_VALUE,
    val speed: Float = 1.0f,
    val isPlaying: Boolean = false,
    val isBuffering: Boolean = true,
    val controlsVisible: Boolean = true,
    val positionMs: Long = 0,
    val bufferedMs: Long = 0,
    val durationMs: Long = 0,
    val offline: Boolean = false,
    val resumeMessage: String? = null,
    /** 2.0 兼容性/解码提示，展示一次后可清空 */
    val compatNotice: String? = null,
    /** 只读弹幕 */
    val danmakuEnabled: Boolean = true,
    val danmakuSupported: Boolean = false,
    val danmakuItems: List<com.juku.app.domain.model.DanmakuItem> = emptyList()
)

@HiltViewModel
class PlayerViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    @ApplicationContext private val appContext: Context,
    private val playbackRepository: PlaybackRepository,
    private val historyRepository: HistoryRepository,
    private val downloadRepository: DownloadRepository,
    private val prefs: AppPrefs,
    @com.juku.app.di.PlaybackOkHttp private val playbackOkHttp: okhttp3.OkHttpClient
) : ViewModel() {

    private val dramaId: String = savedStateHandle["dramaId"] ?: ""
    private val requestedEpisode: Int = savedStateHandle["episode"] ?: -1
    val offline: Boolean = savedStateHandle["offline"] ?: false
    private val fromHistory: Boolean = savedStateHandle["fromHistory"] ?: false

    private val _state = MutableStateFlow(
        PlayerUiState(
            offline = offline,
            danmakuEnabled = prefs.danmakuEnabled
        )
    )
    val state: StateFlow<PlayerUiState> = _state.asStateFlow()

    val player: ExoPlayer = buildPlayer()

    private var session: PlaybackSession? = null
    private var currentRun: Long = 0L
    private var currentHls: HlsPlayback? = null
    private val sequence = AtomicLong(0)

    private var heartbeatJob: Job? = null
    private var progressJob: Job? = null
    private var positionTickerJob: Job? = null
    private var prefetchedNext: Boolean = false
    private var preparedNext: Boolean = false
    private var warmNextJob: Job? = null
    private var switchingEpisode: Boolean = false
    private var danmakuJob: Job? = null
    private val loadedDanmakuKeys = HashSet<String>()
    private var nextHlsCache: Pair<Int, HlsPlayback>? = null

    private val playerListener = object : Player.Listener {
        override fun onPlaybackStateChanged(playbackState: Int) {
            _state.update {
                it.copy(
                    isBuffering = playbackState == Player.STATE_BUFFERING,
                    positionMs = player.currentPosition,
                    durationMs = player.duration.coerceAtLeast(0)
                )
            }
            if (playbackState == Player.STATE_READY) {
                // 一就绪就落一次进度，避免只看几秒退出完全没有历史
                viewModelScope.launch { saveLocalHistory() }
            }
            if (playbackState == Player.STATE_ENDED) onEpisodeEnded()
        }

        override fun onIsPlayingChanged(isPlaying: Boolean) {
            _state.update { it.copy(isPlaying = isPlaying) }
            if (!isPlaying) {
                viewModelScope.launch { saveLocalHistory() }
            }
        }

        override fun onPlayerError(error: PlaybackException) {
            _state.update { it.copy(isBuffering = false, error = "播放出错：${error.message ?: "未知错误"}") }
        }
    }

    init {
        player.addListener(playerListener)
        if (offline) {
            startOffline()
        } else {
            startOnline()
        }
    }

    private fun buildPlayer(): ExoPlayer {
        val dataSourceFactory = DefaultDataSource.Factory(
            appContext,
            OkHttpDataSource.Factory(playbackOkHttp)
                // 原始 CDN 直链可能校验 UA；OkHttp 默认 UA 有时被拒
                .setDefaultRequestProperties(
                    mapOf("User-Agent" to "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36")
                )
        )
        // 起播缓冲与分片对齐：HLS 2s/片，bufferForPlayback 2s（约 1 片）就出画；
        // 前向缓冲仍保留 45-90s 吃住 WiFi 抖动。首播不再干等 4s。
        val loadControl = DefaultLoadControl.Builder()
            .setBufferDurationsMs(
                /* minBufferMs = */ 30_000,
                /* maxBufferMs = */ 90_000,
                /* bufferForPlaybackMs = */ 2_000,
                /* bufferForPlaybackAfterRebufferMs = */ 4_000
            )
            .setTargetBufferBytes(48 * 1024 * 1024)
            .setPrioritizeTimeOverSizeThresholds(true)
            .build()
        // 硬解兼容性差的机型自动回退到软解，避免黑屏/卡花屏
        val renderersFactory = DefaultRenderersFactory(appContext)
            .setEnableDecoderFallback(true)
        return ExoPlayer.Builder(appContext, renderersFactory)
            .setLoadControl(loadControl)
            .setMediaSourceFactory(
                DefaultMediaSourceFactory(dataSourceFactory)
                    // m3u8/分片拉取失败多试几次（网络抖动时白名单重试比默认 3 次更耐造）
                    .setLoadErrorHandlingPolicy(DefaultLoadErrorHandlingPolicy(6))
            )
            // 申请音频焦点：来电/其他应用抢焦点时自动暂停恢复
            .setAudioAttributes(
                androidx.media3.common.AudioAttributes.Builder()
                    .setUsage(C.USAGE_MEDIA)
                    .setContentType(C.AUDIO_CONTENT_TYPE_MOVIE)
                    .build(),
                /* handleAudioFocus = */ true
            )
            .setHandleAudioBecomingNoisy(true)
            .setWakeMode(C.WAKE_MODE_NETWORK)
            .build()
    }

    /* ---------------- 在线播放 ---------------- */

    private fun startOnline() {
        viewModelScope.launch {
            try {
                val localHistory = historyRepository.observeHistory().first().find { it.dramaId == dramaId }
                val opened = playbackRepository.open(dramaId, resume = true, fromHistory = fromHistory)
                opened.fold(
                    onSuccess = { s ->
                        session = s
                        val episodes = s.episodes
                        // 指定集数优先；否则优先本地未看完进度，再退回服务端 initialIndex
                        val initial = if (requestedEpisode > 0) {
                            episodes.find { it.episode == requestedEpisode } ?: episodes.firstOrNull()
                        } else if (localHistory != null && !localHistory.completed) {
                            episodes.find { it.episode == localHistory.episode }
                                ?: episodes.find { it.index == s.initialIndex }
                                ?: episodes.firstOrNull()
                        } else {
                            episodes.find { it.index == s.initialIndex } ?: episodes.firstOrNull()
                        }
                        val startPosition = when {
                            requestedEpisode > 0 -> 0.0
                            s.initialPosition > 0 -> s.initialPosition
                            localHistory != null && !localHistory.completed &&
                                initial?.episode == localHistory.episode -> localHistory.position
                            else -> 0.0
                        }
                        _state.update {
                            it.copy(
                                loading = false,
                                title = s.title,
                                episodes = episodes,
                                resumeMessage = s.resumeMessage
                                    ?: if (startPosition > 0 && requestedEpisode <= 0) {
                                        "已从 ${formatDurationSeconds(startPosition)} 继续播放"
                                    } else null
                            )
                        }
                        if (initial != null) {
                            playEpisodeInternal(initial, startPosition, keepQuality = true)
                        } else {
                            _state.update { it.copy(loading = false, error = "没有可播放的分集") }
                        }
                    },
                    onFailure = { e ->
                        _state.update { it.copy(loading = false, error = e.apiMessage()) }
                    }
                )
            } catch (e: Exception) {
                _state.update { it.copy(loading = false, error = e.apiMessage()) }
            }
        }
    }

    private fun playEpisodeInternal(episode: Episode, startSeconds: Double, keepQuality: Boolean) {
        val s = session ?: return
        switchingEpisode = true
        prefetchedNext = false
        preparedNext = false
        loadedDanmakuKeys.clear()
        danmakuJob?.cancel()
        _state.update {
            it.copy(
                currentEpisode = episode.episode,
                episodeTitle = episode.title,
                isBuffering = true,
                error = null,
                positionMs = 0,
                durationMs = 0,
                danmakuItems = emptyList(),
                danmakuSupported = episode.hasDanmaku
            )
        }
        viewModelScope.launch {
            try {
                val quality = if (keepQuality) {
                    _state.value.currentQuality.takeIf { it != Int.MIN_VALUE } ?: prefs.defaultQuality
                } else {
                    _state.value.currentQuality
                }
                // 与取流并行 prepare 当前集：服务端可提前备好，互不等待
                val prepareJob = viewModelScope.launch {
                    playbackRepository.prepareNext(s.session, episode.episode)
                }
                // 命中预开流则跳过一次网络往返，换集更跟手
                val cached = nextHlsCache?.takeIf { it.first == episode.episode }
                val hls = if (cached != null && startSeconds <= 0) {
                    nextHlsCache = null
                    Result.success(cached.second)
                } else {
                    nextHlsCache = null
                    playbackRepository.openHls(
                        session = s.session,
                        episode = episode.episode,
                        start = startSeconds.takeIf { it > 0 },
                        quality = quality
                    )
                }
                prepareJob.join()
                hls.fold(
                    onSuccess = { playback ->
                        currentRun = playback.run
                        currentHls = playback
                        val startMs = if (startSeconds > 0) (startSeconds * 1000).toLong() else 0L
                        // 后端「优先原始 MP4 / HLS」会下发 CDN 直链，按地址/MIME 识别，
                        // 不能再写死 APPLICATION_M3U8（原片 mp4 会被当成 HLS 解析失败）
                        val mediaItemBuilder = MediaItem.Builder()
                            .setUri(playback.url)
                            .setMimeType(com.juku.app.util.mediaMimeType(playback.url, playback.mimeType))
                        player.setMediaItem(mediaItemBuilder.build(), startMs)
                        player.playbackParameters = PlaybackParameters(_state.value.speed)
                        player.prepare()
                        player.playWhenReady = true
                        _state.update {
                            it.copy(
                                isBuffering = false,
                                qualities = playback.qualities.ifEmpty { it.qualities },
                                currentQuality = playback.quality,
                                compatNotice = playback.compatNotice
                            )
                        }
                        switchingEpisode = false
                        startHeartbeat()
                        startProgressReporting()
                        startPositionTicker()
                        // 当前集一开播就轻量预热下一集，不等当前缓冲打满、也不等进度轮询
                        warmNextEpisodeLight()
                    },
                    onFailure = { e ->
                        _state.update { it.copy(isBuffering = false, error = e.apiMessage()) }
                        switchingEpisode = false
                    }
                )
            } catch (e: Exception) {
                _state.update { it.copy(isBuffering = false, error = e.apiMessage()) }
                switchingEpisode = false
            }
        }
    }

    /* ---------------- 离线播放 ---------------- */

    private fun startOffline() {
        viewModelScope.launch {
            try {
                val tasks = downloadRepository.downloadedEpisodes(dramaId)
                if (tasks.isEmpty()) {
                    _state.update { it.copy(loading = false, error = "没有已下载的分集") }
                    return@launch
                }
                val sorted = tasks.sortedBy { it.episode }
                val episodes = sorted.map { task ->
                    Episode(
                        index = task.episode,
                        episode = task.episode,
                        title = task.title.ifBlank { "第%02d集".format(task.episode) },
                        vip = false
                    )
                }
                val localHistory = historyRepository.observeHistory().first().find { it.dramaId == dramaId }
                val first = when {
                    requestedEpisode > 0 ->
                        sorted.find { it.episode == requestedEpisode } ?: sorted.first()
                    localHistory != null ->
                        sorted.find { it.episode == localHistory.episode } ?: sorted.first()
                    else -> sorted.first()
                }
                val startSeconds = if (
                    requestedEpisode <= 0 &&
                    localHistory != null &&
                    !localHistory.completed &&
                    localHistory.episode == first.episode
                ) localHistory.position else 0.0
                _state.update {
                    it.copy(
                        loading = false,
                        title = first.dramaTitle,
                        episodes = episodes,
                        resumeMessage = if (startSeconds > 0) {
                            "已从 ${formatDurationSeconds(startSeconds)} 继续播放"
                        } else null
                    )
                }
                playOfflineTask(first, startSeconds)
            } catch (e: Exception) {
                _state.update { it.copy(loading = false, error = e.apiMessage()) }
            }
        }
    }

    private fun playOfflineTask(task: DownloadTask, startSeconds: Double = 0.0) {
        val file = task.filePath?.let { java.io.File(it) }
        if (file == null || !file.exists()) {
            _state.update { it.copy(error = "本地文件不存在") }
            return
        }
        _state.update {
            it.copy(
                currentEpisode = task.episode,
                episodeTitle = task.title,
                isBuffering = true,
                error = null,
                positionMs = if (startSeconds > 0) (startSeconds * 1000).toLong() else 0L,
                durationMs = 0
            )
        }
        val mediaItem = MediaItem.Builder()
            .setUri(android.net.Uri.parse("file://" + file.absolutePath))
            .build()
        player.setMediaItem(mediaItem, if (startSeconds > 0) (startSeconds * 1000).toLong() else 0L)
        player.playbackParameters = PlaybackParameters(_state.value.speed)
        player.prepare()
        player.playWhenReady = true
        _state.update { it.copy(isBuffering = false) }
        startPositionTicker()
        viewModelScope.launch { saveLocalHistory() }
    }

    /* ---------------- 周期任务 ---------------- */

    private fun startHeartbeat() {
        heartbeatJob?.cancel()
        val s = session ?: return
        heartbeatJob = viewModelScope.launch {
            while (isActive && !offline) {
                delay(60_000)
                playbackRepository.heartbeat(s.session)
            }
        }
    }

    private fun startProgressReporting() {
        progressJob?.cancel()
        val s = session ?: return
        progressJob = viewModelScope.launch {
            while (isActive && !offline) {
                delay(5_000)
                val st = _state.value
                if (st.isPlaying && currentRun != 0L && st.durationMs > 0) {
                    reportProgress(s.session, completed = false)
                    maybePrefetch(st)
                }
            }
        }
    }

    private suspend fun reportProgress(sessionId: String, completed: Boolean) {
        val st = _state.value
        val positionSec = st.positionMs / 1000.0
        val durationSec = st.durationMs / 1000.0
        playbackRepository.reportProgress(
            session = sessionId,
            run = currentRun,
            sequence = sequence.incrementAndGet(),
            episode = st.currentEpisode,
            position = positionSec,
            duration = durationSec,
            completed = completed
        )
        saveLocalHistory(positionSec, durationSec, completed)
    }

    /**
     * 开播立刻暖下一集。
     * 服务端硬限制：prefetch 只允许 N+1，且要求「当前集缓冲完成」才受理。
     * 所以这里 prepare 后轮询，当前集一缓冲满就立刻预缓存开头；N+2 服务端不允许 prefetch。
     */
    private fun warmNextEpisodeLight() {
        if (offline) return
        val s = session ?: return
        val next = _state.value.episodes.find { it.episode == _state.value.currentEpisode + 1 } ?: return
        warmNextJob?.cancel()
        warmNextJob = viewModelScope.launch {
            if (!preparedNext) {
                preparedNext = true
                runCatching { playbackRepository.prepareNext(s.session, next.episode) }
            }
            // 每 4s 看一眼：当前集缓冲满了就立刻 prefetch N+1（开头先生效），不必等到片尾
            while (isActive && !prefetchedNext) {
                val st = _state.value
                val dur = st.durationMs
                val buffered = st.bufferedMs
                val run = currentRun
                if (run != 0L && dur > 0 && buffered + 500 >= dur) {
                    val ok = runCatching {
                        playbackRepository.prefetch(s.session, next.episode, run)
                    }.getOrNull()?.isSuccess == true
                    if (ok) {
                        prefetchedNext = true
                        break
                    }
                }
                delay(4000)
            }
        }
    }

    private suspend fun maybePrefetch(st: PlayerUiState) {
        val nextEpisode = st.episodes.find { it.episode == st.currentEpisode + 1 } ?: return
        val remainingMs = st.durationMs - st.positionMs
        if (remainingMs <= 0) return
        // 片尾兜底：上面轮询若因短暂未满缓冲失败，临近结束再试
        if (!prefetchedNext && currentRun != 0L && remainingMs <= 90_000) {
            playbackRepository.prefetch(
                session = session?.session ?: return,
                episode = nextEpisode.episode,
                run = currentRun
            ).onSuccess { prefetchedNext = true }
        }
        // 本地预开下一集播放地址，换集少一次网络往返（仍只做 N+1）
        if (nextHlsCache?.first != nextEpisode.episode && currentRun != 0L && remainingMs <= 45_000) {
            val sid = session?.session ?: return
            val opened = playbackRepository.openHls(
                session = sid,
                episode = nextEpisode.episode,
                start = null,
                quality = st.currentQuality.takeIf { it != Int.MIN_VALUE }
            ).getOrNull()
            if (opened != null) nextHlsCache = nextEpisode.episode to opened
        }
    }

    fun toggleDanmaku() {
        val next = !prefs.danmakuEnabled
        prefs.danmakuEnabled = next
        android.util.Log.d("JukuDanmaku", "toggle -> $next supported=${_state.value.danmakuSupported}")
        _state.update { it.copy(danmakuEnabled = next, danmakuItems = if (next) it.danmakuItems else emptyList()) }
        if (next) {
            val st = _state.value
            loadedDanmakuKeys.clear()
            loadDanmakuWindow(st.currentEpisode, st.positionMs, st.durationMs.coerceAtLeast(1))
        }
    }

    private fun loadDanmakuWindow(episode: Int, posMs: Long, durationMs: Long) {
        val sid = session?.session ?: return
        if (!prefs.danmakuEnabled || offline) return
        val start = (posMs / 30_000L) * 30_000L
        val key = "$episode:$start"
        if (!loadedDanmakuKeys.add(key)) return
        danmakuJob?.cancel()
        danmakuJob = viewModelScope.launch {
            val items = playbackRepository.fetchDanmaku(sid, episode, start, durationMs)
            if (items.isNotEmpty()) {
                _state.update { st ->
                    val merged = (st.danmakuItems + items).distinctBy { it.id }
                    st.copy(danmakuItems = merged)
                }
            }
        }
    }

    private fun startPositionTicker() {
        positionTickerJob?.cancel()
        positionTickerJob = viewModelScope.launch {
            while (isActive) {
                delay(500)
                // 暂停时也刷新：前向缓冲仍在增长，进度条要能画出已缓冲区间
                _state.update {
                    it.copy(
                        positionMs = player.currentPosition,
                        bufferedMs = player.bufferedPosition.coerceAtLeast(0),
                        durationMs = player.duration.coerceAtLeast(0)
                    )
                }
                val st = _state.value
                if (st.danmakuEnabled && st.durationMs > 0) {
                    loadDanmakuWindow(st.currentEpisode, st.positionMs, st.durationMs)
                }
            }
        }
    }

    private fun onEpisodeEnded() {
        val st = _state.value
        val s = session
        if (!offline && s != null && currentRun != 0L) {
            viewModelScope.launch { reportProgress(s.session, completed = true) }
        } else {
            viewModelScope.launch {
                saveLocalHistory(st.durationMs / 1000.0, st.durationMs / 1000.0, true)
            }
        }
        val next = st.episodes.find { it.episode == st.currentEpisode + 1 }
        if (next != null) {
            if (offline) {
                viewModelScope.launch {
                    val tasks = downloadRepository.downloadedEpisodes(dramaId)
                    tasks.find { it.episode == next.episode }?.let { playOfflineTask(it) }
                }
            } else {
                playEpisodeInternal(next, 0.0, keepQuality = true)
            }
        }
    }

    private suspend fun saveLocalHistory(position: Double = -1.0, duration: Double = 0.0, completed: Boolean = false) {
        val st = _state.value
        historyRepository.saveLocal(
            dramaId = dramaId,
            source = dramaId.substringBefore(':', ""),
            title = st.title,
            episode = st.currentEpisode,
            index = st.currentEpisode,
            total = st.episodes.size,
            position = if (position >= 0) position else st.positionMs / 1000.0,
            duration = if (duration > 0) duration else st.durationMs / 1000.0,
            completed = completed
        )
    }

    /* ---------------- UI 调用 ---------------- */

    fun playEpisode(episodeNumber: Int) {
        if (offline) {
            viewModelScope.launch {
                downloadRepository.downloadedEpisodes(dramaId).find { it.episode == episodeNumber }
                    ?.let { playOfflineTask(it) }
            }
        } else {
            val episode = _state.value.episodes.find { it.episode == episodeNumber } ?: return
            if (episode.episode == _state.value.currentEpisode) return
            playEpisodeInternal(episode, 0.0, keepQuality = true)
        }
    }

    fun playPrevious() {
        val st = _state.value
        st.episodes.find { it.episode == st.currentEpisode - 1 }?.let { playEpisode(it.episode) }
    }

    fun playNext() {
        val st = _state.value
        st.episodes.find { it.episode == st.currentEpisode + 1 }?.let { playEpisode(it.episode) }
    }

    fun togglePlay() {
        if (player.isPlaying) player.pause() else player.play()
    }

    fun seekTo(positionMs: Long) {
        player.seekTo(positionMs.coerceIn(0, player.duration.coerceAtLeast(0)))
        _state.update { it.copy(positionMs = positionMs) }
        viewModelScope.launch { saveLocalHistory() }
    }

    fun seekBy(deltaMs: Long) {
        seekTo(player.currentPosition + deltaMs)
    }

    fun setSpeed(speed: Float) {
        _state.update { it.copy(speed = speed) }
        player.playbackParameters = PlaybackParameters(speed)
    }

    fun setQuality(quality: Int) {
        if (offline) return
        val st = _state.value
        if (quality == st.currentQuality) return
        _state.update { it.copy(currentQuality = quality) }
        val episode = st.episodes.find { it.episode == st.currentEpisode } ?: return
        val startSeconds = st.positionMs / 1000.0
        playEpisodeInternal(episode, startSeconds, keepQuality = false)
    }

    fun toggleControls() = _state.update { it.copy(controlsVisible = !it.controlsVisible) }
    fun showControls() = _state.update { it.copy(controlsVisible = true) }
    fun hideControls() = _state.update { it.copy(controlsVisible = false) }

    fun consumeResumeMessage() = _state.update { it.copy(resumeMessage = null) }
    fun consumeCompatNotice() = _state.update { it.copy(compatNotice = null) }
    fun dismissError() = _state.update { it.copy(error = null) }

    fun setSpeedWhilePressed(boost: Boolean) {
        if (boost) {
            if (_state.value.speed != 3.0f) player.playbackParameters = PlaybackParameters(3.0f)
        } else {
            player.playbackParameters = PlaybackParameters(_state.value.speed)
        }
    }

    fun persistProgress() {
        val st = _state.value
        if (st.currentEpisode <= 0) return
        val pos = st.positionMs / 1000.0
        val dur = st.durationMs / 1000.0
        val completedFlag = dur > 0 && pos >= dur - 1
        kotlinx.coroutines.CoroutineScope(
            kotlinx.coroutines.Dispatchers.IO + kotlinx.coroutines.SupervisorJob()
        ).launch(kotlinx.coroutines.NonCancellable) {
            historyRepository.saveLocal(
                dramaId = dramaId,
                source = dramaId.substringBefore(':', ""),
                title = st.title,
                episode = st.currentEpisode,
                index = st.currentEpisode,
                total = st.episodes.size,
                position = pos.coerceAtLeast(0.0),
                duration = dur.coerceAtLeast(0.0),
                completed = completedFlag
            )
        }
    }

    override fun onCleared() {
        persistProgress()
        val s = session
        if (s != null && !offline) {
            kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.IO).launch {
                playbackRepository.close(s.session)
            }
        }
        heartbeatJob?.cancel()
        progressJob?.cancel()
        positionTickerJob?.cancel()
        player.removeListener(playerListener)
        player.release()
        super.onCleared()
    }
}
