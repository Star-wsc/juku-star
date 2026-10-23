package com.juku.app.ui.screens.auth

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.domain.repository.AuthRepository
import com.juku.app.util.apiMessage
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class AuthUiState(
    val mode: String = "login",          // login | register
    val serverHost: String = "",
    val serverPort: String = "",
    val username: String = "",
    val password: String = "",
    val busy: Boolean = false,
    val error: String? = null,
    val message: String? = null,
    val success: Boolean = false,
    val allowRegistration: Boolean = true
)

@HiltViewModel
class AuthViewModel @Inject constructor(
    private val prefs: AppPrefs,
    private val authRepository: AuthRepository
) : ViewModel() {

    private val _state = MutableStateFlow(AuthUiState())
    val state: StateFlow<AuthUiState> = _state.asStateFlow()

    init {
        val bare = prefs.serverUrl
        if (bare.isNotBlank()) {
            val host = bare.substringAfter("://", bare)
                .substringBefore("/")
            val port = if (host.contains(":")) host.substringAfter(":") else ""
            val hostname = host.substringBefore(":")
            _state.update {
                it.copy(
                    serverHost = if (bare.contains("://")) bare.substringBefore("://") + "://" + hostname else hostname,
                    serverPort = port
                )
            }
        }
    }

    fun onModeChange(mode: String) = _state.update { it.copy(mode = mode, error = null) }
    fun setMode(mode: String) = onModeChange(mode)

    // 输入过程中不 trimEnd('/')，否则 https:// 的 // 敲一个被删一个
    fun onServerHostChange(value: String) = _state.update {
        it.copy(serverHost = value.trim(), error = null)
    }

    fun onPortChange(value: String) = _state.update {
        it.copy(serverPort = value.filter { c -> c.isDigit() }, error = null)
    }

    fun onServerPortChange(value: String) = onPortChange(value)

    fun submit() = connect()

    fun consumeSuccess() = _state.update { it.copy(success = false) }

    fun onUsernameChange(value: String) = _state.update { it.copy(username = value.trim(), error = null) }
    fun onPasswordChange(value: String) = _state.update { it.copy(password = value, error = null) }

    fun connect() {
        val s = _state.value
        if (s.busy || s.serverHost.isBlank()) return
        _state.update { it.copy(busy = true, error = null) }
        viewModelScope.launch {
            val previous = prefs.serverUrl
            prefs.serverUrl = buildServerUrl(s.serverHost, s.serverPort)
            authRepository.bootstrap().fold(
                onSuccess = {
                    val hasCreds = s.username.isNotBlank() && s.password.isNotBlank()
                    val result = when {
                        hasCreds && s.mode == "login" ->
                            authRepository.login(s.username, s.password)
                        hasCreds ->
                            authRepository.register(s.username, s.password)
                        else -> Result.success(Unit)
                    }
                    result.fold(
                        onSuccess = {
                            prefs.username = s.username.ifBlank { prefs.username }
                            prefs.hasEnteredApp = true
                            _state.update { it.copy(busy = false, success = true) }
                        },
                        onFailure = { e ->
                            prefs.serverUrl = previous
                            _state.update { it.copy(busy = false, error = e.apiMessage()) }
                        }
                    )
                },
                onFailure = { e ->
                    prefs.serverUrl = previous
                    _state.update { it.copy(busy = false, error = e.apiMessage()) }
                }
            )
        }
    }

    /**
     * 支持反代 HTTPS 域名与传统 http://IP:端口：
     * - https://domain.com（端口可空，默认 443）
     * - http://192.168.1.10:8998
     * - 只填域名且端口空 → https://域名
     * - 只填 IP/域名且写了端口 → http://host:port
     */
    private fun buildServerUrl(hostInput: String, portInput: String): String {
        val raw = hostInput.trim().trimEnd('/')
        val port = portInput.trim()
        val lower = raw.lowercase()
        val scheme = when {
            lower.startsWith("https://") -> "https"
            lower.startsWith("http://") -> "http"
            port.isNotEmpty() -> "http"
            else -> "https"
        }
        val bare = raw.removePrefix("https://").removePrefix("http://")
            .removePrefix("HTTPS://").removePrefix("HTTP://").trimEnd('/')
        val hostOnly = bare.substringBefore("/")
        return if (port.isNotEmpty() && !hostOnly.contains(":")) {
            "$scheme://$hostOnly:$port"
        } else {
            "$scheme://$hostOnly"
        }
    }

    fun consumeMessage() = _state.update { it.copy(message = null) }
}
