package com.juku.app.ui.screens.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.domain.repository.AuthRepository
import com.juku.app.domain.repository.DownloadRepository
import com.juku.app.domain.repository.DramaRepository
import com.juku.app.util.apiMessage
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class SettingsUiState(
    val serverUrl: String = "",
    val username: String? = null,
    val themeMode: String = "system",
    val defaultSpeed: Float = 1.0f,
    val showHuangguo: Boolean = true,
    val showHuangdou: Boolean = true,
    val storageUsedBytes: Long = 0,
    val busy: Boolean = false,
    val libraryUpdating: Boolean = false,
    val message: String? = null
)

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val prefs: AppPrefs,
    private val authRepository: AuthRepository,
    private val downloadRepository: DownloadRepository,
    private val dramaRepository: DramaRepository
) : ViewModel() {

    private val _state = MutableStateFlow(
        SettingsUiState(
            serverUrl = prefs.serverUrl,
            username = prefs.username,
            themeMode = prefs.themeMode,
            defaultSpeed = prefs.defaultSpeed,
            showHuangguo = prefs.showHuangguo,
            showHuangdou = prefs.showHuangdou
        )
    )
    val state: StateFlow<SettingsUiState> = _state.asStateFlow()

    init {
        viewModelScope.launch {
            downloadRepository.storageUsedBytes().let { used ->
                _state.update { it.copy(storageUsedBytes = used) }
            }
        }
    }

    fun setTheme(mode: String) {
        prefs.themeMode = mode
        _state.update { it.copy(themeMode = mode) }
    }

    fun setDefaultSpeed(speed: Float) {
        prefs.defaultSpeed = speed
        _state.update { it.copy(defaultSpeed = speed) }
    }

    fun setShowHuangguo(show: Boolean) {
        prefs.showHuangguo = show
        _state.update { it.copy(showHuangguo = show) }
    }

    fun setShowHuangdou(show: Boolean) {
        prefs.showHuangdou = show
        _state.update { it.copy(showHuangdou = show) }
    }

    fun logout(onDone: () -> Unit) {
        viewModelScope.launch {
            authRepository.logout().fold(
                onSuccess = {
                    _state.update { it.copy(username = prefs.username, message = "已退出登录") }
                    onDone()
                },
                onFailure = { e ->
                    _state.update { it.copy(message = e.apiMessage()) }
                }
            )
        }
    }

    fun changePassword(oldPassword: String, newPassword: String, onDone: () -> Unit) {
        viewModelScope.launch {
            authRepository.changePassword(oldPassword, newPassword).fold(
                onSuccess = {
                    _state.update { it.copy(message = "密码修改成功") }
                    onDone()
                },
                onFailure = { e ->
                    _state.update { it.copy(message = e.apiMessage()) }
                }
            )
        }
    }

    /**
     * 从源站更新剧库。已在更新中则忽略，避免重复点。
     * 后端同时只会跑一个更新任务。
     */
    fun updateLibrary() {
        if (_state.value.libraryUpdating) return
        viewModelScope.launch {
            _state.update { it.copy(libraryUpdating = true, message = "正在从源站更新剧库…") }
            dramaRepository.updateLibrary().fold(
                onSuccess = { started ->
                    _state.update {
                        it.copy(
                            libraryUpdating = started,
                            message = if (started) "已开始更新，拉取中可继续用 App，稍后下拉刷新查看"
                            else "剧库已在更新中，请稍候"
                        )
                    }
                },
                onFailure = { e ->
                    _state.update {
                        it.copy(libraryUpdating = false, message = e.apiMessage())
                    }
                }
            )
        }
    }

    fun consumeMessage() = _state.update { it.copy(message = null) }
}
