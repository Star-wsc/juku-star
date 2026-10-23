package com.juku.app.ui.screens.auth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle

/**
 * 统一登录/连接页（整页，非弹窗）。
 *
 * 上半部分：服务器地址（IP + 端口）；
 * 下半部分：账号密码（登录 / 注册）。
 * 服务器无需登录时可只填服务器直接进入；需要登录时必须输入账密。
 */
@Composable
fun AuthScreen(
    onEntered: () -> Unit,
    viewModel: AuthViewModel = hiltViewModel()
) {
    val state by viewModel.state.collectAsStateWithLifecycle()

    // 阻止系统密码管理器自动填充
    val view = LocalView.current
    DisposableEffect(view) {
        val previous = view.importantForAutofill
        view.importantForAutofill = android.view.View.IMPORTANT_FOR_AUTOFILL_NO
        onDispose { view.importantForAutofill = previous }
    }

    if (state.success) {
        viewModel.consumeSuccess()
        onEntered()
        return
    }

    // 背景/文字全部走 colorScheme，避免浅色模式下落到系统 windowBackground 或默认黑色
    Surface(
        modifier = Modifier
            .fillMaxSize()
            .windowInsetsPadding(WindowInsets.safeDrawing),
        color = MaterialTheme.colorScheme.background
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 32.dp, vertical = 24.dp),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Spacer(Modifier.height(48.dp))

            // ── 品牌标题 ──
            Text(
                "短剧库",
                style = MaterialTheme.typography.headlineLarge,
                color = MaterialTheme.colorScheme.onBackground
            )
            Text(
                "Juku Drama",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )

            Spacer(Modifier.height(40.dp))

            // ── 服务器 ──
            Text(
                "服务器",
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.fillMaxWidth()
            )
            Spacer(Modifier.height(8.dp))
            Row(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth()
            ) {
                OutlinedTextField(
                    value = state.serverHost,
                    onValueChange = viewModel::onServerHostChange,
                    label = { Text("IP / 域名") },
                    placeholder = { Text("192.168.1.10") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(
                        capitalization = KeyboardCapitalization.None,
                        autoCorrectEnabled = false,
                        keyboardType = KeyboardType.Uri
                    ),
                    modifier = Modifier.weight(2f)
                )
                OutlinedTextField(
                    value = state.serverPort,
                    onValueChange = viewModel::onServerPortChange,
                    label = { Text("端口") },
                    placeholder = { Text("8998") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.weight(1f)
                )
            }

            Spacer(Modifier.height(28.dp))
            HorizontalDivider()
            Spacer(Modifier.height(20.dp))

            // ── 账号 ──
            Text(
                "账号",
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.fillMaxWidth()
            )
            Spacer(Modifier.height(8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(
                    selected = state.mode == "login",
                    onClick = { viewModel.setMode("login") },
                    label = { Text("登录") }
                )
                if (state.allowRegistration) {
                    FilterChip(
                        selected = state.mode == "register",
                        onClick = { viewModel.setMode("register") },
                        label = { Text("注册") }
                    )
                }
            }
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = state.username,
                onValueChange = viewModel::onUsernameChange,
                label = { Text("用户名") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(
                    capitalization = KeyboardCapitalization.None,
                    autoCorrectEnabled = false,
                    keyboardType = KeyboardType.Text
                ),
                modifier = Modifier.fillMaxWidth()
            )
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = state.password,
                onValueChange = viewModel::onPasswordChange,
                label = { Text("密码") },
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(
                    capitalization = KeyboardCapitalization.None,
                    keyboardType = KeyboardType.Password,
                    autoCorrectEnabled = false
                ),
                modifier = Modifier.fillMaxWidth()
            )

            // ── 错误提示 ──
            if (state.error != null) {
                Spacer(Modifier.height(12.dp))
                Text(
                    state.error ?: "",
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.fillMaxWidth()
                )
            }

            Spacer(Modifier.height(32.dp))

            // ── 进入按钮 ──
            Button(
                onClick = viewModel::submit,
                enabled = !state.busy && state.serverHost.isNotBlank(),
                modifier = Modifier
                    .fillMaxWidth()
                    .height(48.dp)
            ) {
                if (state.busy) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(20.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.onPrimary
                    )
                    Spacer(Modifier.width(8.dp))
                    Text("连接中…", color = MaterialTheme.colorScheme.onPrimary)
                } else {
                    Text(
                        if (state.username.isNotBlank() && state.password.isNotBlank()) {
                            if (state.mode == "login") "登录并进入" else "注册并进入"
                        } else {
                            "连接进入"
                        },
                        style = MaterialTheme.typography.titleMedium,
                        color = MaterialTheme.colorScheme.onPrimary
                    )
                }
            }

            if (state.username.isBlank() || state.password.isBlank()) {
                Spacer(Modifier.height(8.dp))
                Text(
                    "该服务器无需登录时可直接连接",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
    }
}
