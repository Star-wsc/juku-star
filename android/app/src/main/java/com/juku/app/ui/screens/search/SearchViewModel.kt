package com.juku.app.ui.screens.search

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.data.repository.DramaRepositoryImpl
import com.juku.app.domain.model.Drama
import com.juku.app.util.apiMessage
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class SearchUiState(
    val query: String = "",
    val localResults: List<Drama> = emptyList(),
    val onlineResults: List<Drama> = emptyList(),
    val suggestions: List<String> = emptyList(),
    val searching: Boolean = false,
    val error: String? = null
)

@OptIn(FlowPreview::class)
@HiltViewModel
class SearchViewModel @Inject constructor(
    private val dramaRepository: DramaRepositoryImpl,
    private val prefs: AppPrefs
) : ViewModel() {

    private val _state = MutableStateFlow(SearchUiState())
    val state: StateFlow<SearchUiState> = _state.asStateFlow()

    private val queryFlow = MutableStateFlow("")
    private val suggestFlow = MutableStateFlow("")

    init {
        viewModelScope.launch {
            queryFlow
                .debounce(400)
                .distinctUntilChanged()
                .collect { q -> searchOnline(q) }
        }
        // 2.0 搜索联想：更短防抖；1.0 失败静默
        viewModelScope.launch {
            suggestFlow
                .debounce(250)
                .distinctUntilChanged()
                .collect { q -> loadSuggestions(q) }
        }
        // 缓存变化（含切换服务器 resetCache）时自动刷新本地搜索结果，避免跨服务器污染
        viewModelScope.launch {
            dramaRepository.cachedDramas().collect {
                val q = _state.value.query
                val local = localSearch(q)
                _state.update { s -> s.copy(localResults = local) }
            }
        }
    }

    fun onQueryChange(value: String) {
        _state.update { it.copy(query = value, error = null) }
        viewModelScope.launch {
            val local = localSearch(value)
            _state.update { it.copy(localResults = local) }
        }
        val trimmed = value.trim()
        queryFlow.value = trimmed
        suggestFlow.value = trimmed
    }

    /** 点击联想条目：填入关键词并触发主搜索 */
    fun onSuggestionClick(text: String) {
        onQueryChange(text)
    }

    private suspend fun loadSuggestions(query: String) {
        if (query.isEmpty()) {
            _state.update { it.copy(suggestions = emptyList()) }
            return
        }
        val items = dramaRepository.searchSuggestions(query)
            .filter { !it.equals(query, ignoreCase = true) }
            .take(8)
        _state.update { it.copy(suggestions = items) }
    }

    private suspend fun localSearch(query: String): List<Drama> {
        val q = query.trim()
        if (q.isEmpty()) return emptyList()
        return try {
            dramaRepository.cachedDramas().first()
                .filter { it.title.contains(q, ignoreCase = true) }
                .filter { !prefs.isSourceHidden(it.source) }
        } catch (_: Exception) {
            emptyList()
        }
    }

    private fun searchOnline(query: String) {
        if (query.isEmpty()) {
            _state.update { it.copy(onlineResults = emptyList(), searching = false) }
            return
        }
        viewModelScope.launch {
            _state.update { it.copy(searching = true) }
            try {
                val results = dramaRepository.searchOnline(query)
                    .filter { !prefs.isSourceHidden(it.source) }
                _state.update { it.copy(searching = false, onlineResults = results) }
            } catch (e: Exception) {
                _state.update { it.copy(searching = false, error = e.apiMessage()) }
            }
        }
    }
}
