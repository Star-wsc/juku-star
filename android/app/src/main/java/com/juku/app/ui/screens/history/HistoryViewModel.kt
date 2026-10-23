package com.juku.app.ui.screens.history

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.domain.model.HistoryItem
import com.juku.app.domain.repository.HistoryRepository
import com.juku.app.util.apiMessage
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class HistoryUiState(
    val items: List<HistoryItem> = emptyList(),
    val loading: Boolean = true,
    val message: String? = null,
    val confirmClear: Boolean = false
)

@HiltViewModel
class HistoryViewModel @Inject constructor(
    private val historyRepository: HistoryRepository,
    private val prefs: AppPrefs
) : ViewModel() {

    private val _state = MutableStateFlow(HistoryUiState())
    val state: StateFlow<HistoryUiState> = _state.asStateFlow()

    init {
        viewModelScope.launch {
            combine(
                historyRepository.observeHistory(),
                prefs.showHuangguoState,
                prefs.showHuangdouState
            ) { items, showHuangguo, showHuangdou ->
                items.filter { !AppPrefs.isSourceHiddenFor(showHuangguo, showHuangdou, it.source) }
            }.collect { items ->
                _state.update { it.copy(items = items, loading = false) }
            }
        }
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            historyRepository.refreshFromServer().onFailure { e ->
                _state.update { it.copy(message = e.apiMessage()) }
            }
        }
    }

    fun remove(dramaId: String) {
        viewModelScope.launch {
            historyRepository.remove(dramaId).onFailure { e ->
                _state.update { it.copy(message = e.apiMessage()) }
            }
        }
    }

    fun requestClearAll() = _state.update { it.copy(confirmClear = true) }
    fun cancelClearAll() = _state.update { it.copy(confirmClear = false) }

    fun clearAll() {
        _state.update { it.copy(confirmClear = false) }
        viewModelScope.launch {
            historyRepository.removeAll().onFailure { e ->
                _state.update { it.copy(message = e.apiMessage()) }
            }
        }
    }

    fun consumeMessage() = _state.update { it.copy(message = null) }
}
