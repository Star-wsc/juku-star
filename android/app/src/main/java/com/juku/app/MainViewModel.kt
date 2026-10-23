package com.juku.app

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.domain.repository.AuthRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class MainUiState(
    /** false = 显示 AuthScreen（登录/连接页），true = 显示主界面 */
    val showAuth: Boolean = true,
    val bootstrapError: String? = null
)

@HiltViewModel
class MainViewModel @Inject constructor(
    private val prefs: AppPrefs,
    private val authRepository: AuthRepository
) : ViewModel() {

    val themeMode: StateFlow<String> get() = prefs.themeModeState

    private val _state = MutableStateFlow(MainUiState())
    val state: StateFlow<MainUiState> = _state.asStateFlow()

    init {
        if (prefs.serverUrl.isNotBlank() && prefs.hasEnteredApp) {
            // 已有服务器配置且曾经进入过：尝试自动登录（Cookie 持久化恢复）
            bootstrap()
        }
        viewModelScope.launch {
            // 身份变化：仅在"服务器要求登录但当前未登录"时拉回登录页
            authRepository.viewerState.collect { viewer ->
                if (viewer != null && viewer.requireLogin && !viewer.isLoggedIn) {
                    _state.update { it.copy(showAuth = true) }
                }
            }
        }
    }

    fun bootstrap() {
        viewModelScope.launch {
            authRepository.bootstrap().fold(
                onSuccess = { viewer ->
                    val ok = !viewer.requireLogin || viewer.isLoggedIn
                    _state.update { it.copy(showAuth = !prefs.hasEnteredApp || !ok, bootstrapError = null) }
                },
                onFailure = { e ->
                    _state.update { it.copy(showAuth = true, bootstrapError = e.message) }
                }
            )
        }
    }

    /** AuthScreen 连接/登录成功后调用 */
    fun onAuthEntered() {
        _state.update { it.copy(showAuth = false) }
    }

    /** 设置页退出登录后调用：强制回到登录页 */
    fun onLoggedOut() {
        prefs.hasEnteredApp = false
        _state.update { it.copy(showAuth = true) }
    }
}
