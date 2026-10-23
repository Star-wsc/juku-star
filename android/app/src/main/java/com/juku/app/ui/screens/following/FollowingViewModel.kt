package com.juku.app.ui.screens.following

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.domain.model.FollowingItem
import com.juku.app.domain.model.HistoryItem
import com.juku.app.domain.model.isUnfinished
import com.juku.app.domain.repository.FollowingRepository
import com.juku.app.domain.repository.HistoryRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

data class FollowingUiState(
    val loading: Boolean = true,
    val error: String? = null,
    val watching: List<FollowingItem> = emptyList(),
    val want: List<FollowingItem> = emptyList(),
    val watched: List<FollowingItem> = emptyList(),
    val historyByDrama: Map<String, HistoryItem> = emptyMap()
)

@HiltViewModel
class FollowingViewModel @Inject constructor(
    private val followingRepository: FollowingRepository,
    private val historyRepository: HistoryRepository,
    private val prefs: AppPrefs
) : ViewModel() {

    val state: StateFlow<FollowingUiState> =
        combine(
            followingRepository.observeFollowing(),
            historyRepository.observeHistory(),
            prefs.showHuangguoState,
            prefs.showHuangdouState
        ) { following, history, showHuangguo, showHuangdou ->
            val historyIds = history.map { it.dramaId }.toSet()
            val unfinishedIds = history.filter { it.isUnfinished() }.map { it.dramaId }.toSet()
            // 已屏蔽站源的条目不出现在任何标签里
            val visible = following.filter {
                !AppPrefs.isSourceHiddenFor(showHuangguo, showHuangdou, it.source)
            }
            FollowingUiState(
                loading = false,
                want = visible.filter { it.saved && !it.completed && it.dramaId !in historyIds },
                watching = visible.filter { it.saved && !it.completed && it.dramaId in unfinishedIds },
                watched = visible.filter {
                    it.completed || (it.saved && it.dramaId in historyIds && it.dramaId !in unfinishedIds)
                },
                historyByDrama = history.associateBy { it.dramaId }
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), FollowingUiState())

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            followingRepository.refresh()
            historyRepository.refreshFromServer()
        }
    }

    fun acknowledge(dramaId: String) {
        viewModelScope.launch {
            followingRepository.acknowledgeNew(dramaId)
        }
    }
}
