package com.juku.app.ui.screens.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/** 红果版内容源说明：不出现其他站源名称 */
@Composable
fun ContentSourceSettingsBody(state: SettingsUiState, viewModel: SettingsViewModel) {
    Column(
        Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surface)
            .padding(16.dp)
    ) {
        Text(
            "本版本仅提供红果内容，与账号权限无关。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )
    }
}

@Composable
fun libraryUpdateHint(updating: Boolean): String =
    if (updating) "正在拉取最新剧集…" else "从红果拉取新剧与资料；更新期间请勿重复点击。"
