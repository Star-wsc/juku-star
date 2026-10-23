package com.juku.app.ui.screens.detail

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.data.repository.DramaRepositoryImpl
import com.juku.app.domain.model.Drama
import com.juku.app.domain.model.DownloadTask
import com.juku.app.domain.model.Episode
import com.juku.app.domain.model.FollowState
import com.juku.app.domain.model.HistoryItem
import com.juku.app.domain.model.PlaybackSession
import com.juku.app.domain.model.QualityOption
import com.juku.app.domain.repository.DownloadRepository
import com.juku.app.domain.repository.FollowingRepository
import com.juku.app.domain.repository.HistoryRepository
import com.juku.app.domain.repository.PlaybackRepository
import com.juku.app.util.apiMessage
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class DetailUiState(
    val dramaId: String = "",
    val drama: Drama? = null,
    val loading: Boolean = true,
    val error: String? = null,
    val episodes: List<Episode> = emptyList(),
    val followState: FollowState = FollowState.NONE,
    val followBusy: Boolean = false,
    val qualities: List<QualityOption>? = null,
    val defaultDownloadQuality: Int = Int.MIN_VALUE,
    val downloaded: Map<Int, DownloadTask> = emptyMap(),
    val history: HistoryItem? = null,
    val message: String? = null
)

@HiltViewModel
class DetailViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val prefs: AppPrefs,
    private val dramaRepository: DramaRepositoryImpl,
    private val playbackRepository: PlaybackRepository,
    private val followingRepository: FollowingRepository,
    private val historyRepository: HistoryRepository,
    private val downloadRepository: DownloadRepository
) : ViewModel() {

    private val dramaId: String = savedStateHandle["dramaId"] ?: ""

    private val _state = MutableStateFlow(DetailUiState(dramaId = dramaId))
    val state: StateFlow<DetailUiState> = _state.asStateFlow()

    private var session: PlaybackSession? = null

    init {
        _state.update { it.copy(defaultDownloadQuality = prefs.downloadQuality) }
        viewModelScope.launch {
            // 先取缓存立显，再向服务器刷新详情
            val cached = dramaRepository.cachedDramas().first().find { it.id == dramaId }
            if (cached != null) {
                _state.update { it.copy(drama = cached, loading = false) }
            }
            refreshDrama()
            loadDownloaded()
            openSession()
        }
        viewModelScope.launch {
            followingRepository.refresh()
            kotlinx.coroutines.flow.combine(
                followingRepository.observeFollowing(),
                historyRepository.observeHistory()
            ) { following, history ->
                val entry = following.find { it.dramaId == dramaId }
                val hist = history.find { it.dramaId == dramaId }
                val watched = entry != null && entry.completed
                val saved = entry?.saved == true
                val followState = when {
                    watched -> FollowState.WATCHED
                    saved && hist != null && !hist.completed -> FollowState.WATCHING
                    saved -> FollowState.WANT
                    else -> FollowState.NONE
                }
                followState to hist
            }.collect { (followState, hist) ->
                _state.update { it.copy(followState = followState, history = hist) }
            }
        }
    }

    private suspend fun refreshDrama() {
        try {
            val fresh = dramaRepository.refreshDrama(dramaId)
            if (fresh != null) {
                _state.update { it.copy(drama = fresh, loading = false, error = null) }
            } else if (_state.value.drama == null) {
                _state.update { it.copy(loading = false, error = "获取剧集信息失败") }
            }
        } catch (e: Exception) {
            if (_state.value.drama == null) {
                _state.update { it.copy(loading = false, error = e.apiMessage()) }
            }
        }
    }

    private suspend fun openSession() {
        try {
            val opened = playbackRepository.open(dramaId, resume = false)
            opened.onSuccess { s ->
                session = s
                _state.update { it.copy(episodes = s.episodes) }
                // 不在此处 openHls 预取画质：用户连点多个详情页会抢带宽、拖慢当前播放。
                // 画质在真正播放时由 hls/open 返回，或点下载面板时再拉。
            }
        } catch (_: Exception) {
        }
    }

    /** 下载面板打开时再拉一次画质（仅此时需要） */
    fun ensureQualities() {
        val s = session ?: return
        if (!_state.value.qualities.isNullOrEmpty()) return
        viewModelScope.launch {
            val firstEpisode = s.episodes.firstOrNull()?.episode ?: return@launch
            playbackRepository.openHls(s.session, firstEpisode).onSuccess { hls ->
                val qs = hls.qualities
                if (!qs.isNullOrEmpty()) {
                    _state.update { it.copy(qualities = qs) }
                }
            }
        }
    }

    private suspend fun loadDownloaded() {
        try {
            val tasks = downloadRepository.downloadedEpisodes(dramaId)
            _state.update {
                it.copy(downloaded = tasks.associateBy { t -> t.episode })
            }
        } catch (_: Exception) {
        }
    }

    fun retry() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            refreshDrama()
            loadDownloaded()
            if (session == null) openSession()
        }
    }

    fun follow() = toggleFollow { followingRepository.follow(dramaId) }
    fun markWatched() = toggleFollow { followingRepository.markWatched(dramaId) }
    fun unfollow() = toggleFollow { followingRepository.unfollow(dramaId) }

    private fun toggleFollow(action: suspend () -> Result<Unit>) {
        if (_state.value.followBusy) return
        viewModelScope.launch {
            _state.update { it.copy(followBusy = true) }
            action().fold(
                onSuccess = { _state.update { s -> s.copy(followBusy = false) } },
                onFailure = { e ->
                    _state.update { s -> s.copy(followBusy = false, message = e.apiMessage()) }
                }
            )
        }
    }

    fun download(episodes: List<Int>, quality: Int) {
        val drama = _state.value.drama ?: return
        if (episodes.isEmpty()) return
        viewModelScope.launch {
            try {
                downloadRepository.enqueue(
                    dramaId = dramaId,
                    dramaTitle = drama.title,
                    source = drama.source,
                    episodes = episodes.map { ep ->
                        val e = _state.value.episodes.find { it.episode == ep }
                        ep to (e?.title ?: "第%02d集".format(ep))
                    },
                    quality = quality
                )
                _state.update { it.copy(message = "已加入下载队列（${episodes.size} 集）") }
            } catch (e: Exception) {
                _state.update { it.copy(message = e.apiMessage()) }
            }
        }
    }

    fun consumeMessage() = _state.update { it.copy(message = null) }

    override fun onCleared() {
        session?.let { s ->
            kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.IO).launch {
                playbackRepository.close(s.session)
            }
        }
        super.onCleared()
    }
}
