package com.juku.app.ui.screens.history

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.juku.app.domain.model.HistoryItem
import com.juku.app.ui.components.EmptyState
import com.juku.app.ui.components.LoadingState
import com.juku.app.util.formatDurationSeconds
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HistoryScreen(
    onOpenDrama: (String) -> Unit,
    onPlay: (String) -> Unit = onOpenDrama,
    viewModel: HistoryViewModel = hiltViewModel()
) {
    val state by viewModel.state.collectAsStateWithLifecycle()

    LaunchedEffect(state.message) {
        if (state.message != null) viewModel.consumeMessage()
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("观看历史") },
                actions = {
                    if (state.items.isNotEmpty()) {
                        IconButton(onClick = viewModel::requestClearAll) {
                            Icon(Icons.Filled.Delete, contentDescription = "清空历史")
                        }
                    }
                }
            )
        }
    ) { padding ->
        when {
            state.loading && state.items.isEmpty() -> LoadingState(Modifier.padding(padding))
            state.items.isEmpty() -> EmptyState(
                message = "还没有观看记录",
                modifier = Modifier.padding(padding)
            )
            else -> LazyColumn(
                contentPadding = androidx.compose.foundation.layout.PaddingValues(12.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
                modifier = Modifier.padding(padding)
            ) {
                items(state.items, key = { it.dramaId }) { item ->
                    HistoryCard(
                        item = item,
                        onClick = { onPlay(item.dramaId) }, // 续播直进播放器（fromHistory=true）
                        onDetail = { onOpenDrama(item.dramaId) },
                        onRemove = { viewModel.remove(item.dramaId) }
                    )
                }
            }
        }
    }

    if (state.confirmClear) {
        AlertDialog(
            onDismissRequest = viewModel::cancelClearAll,
            title = { Text("清空观看历史") },
            text = { Text("确定要清空所有观看历史吗？此操作不可恢复。") },
            confirmButton = {
                TextButton(onClick = viewModel::clearAll) { Text("清空") }
            },
            dismissButton = {
                TextButton(onClick = viewModel::cancelClearAll) { Text("取消") }
            }
        )
    }
}

@Composable
private fun HistoryCard(item: HistoryItem, onClick: () -> Unit, onDetail: () -> Unit, onRemove: () -> Unit) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick) // 点击卡片 = 续播
    ) {
        Row(
            Modifier.padding(14.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(Modifier.weight(1f).clickable(onClick = onDetail)) { // 点击文字区 = 进详情
                Text(item.title, style = MaterialTheme.typography.titleSmall)
                Text(
                    "看到第 ${item.episode} 集 · ${formatDurationSeconds(item.position)}" +
                        (if (item.duration > 0) " / ${formatDurationSeconds(item.duration)}" else "") +
                        if (item.completed) " · 已看完" else "",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                if (!item.completed && item.duration > 0) {
                    Spacer(Modifier.height(6.dp))
                    LinearProgressIndicator(
                        progress = { (item.position / item.duration).toFloat().coerceIn(0f, 1f) },
                        modifier = Modifier.fillMaxWidth()
                    )
                }
                if (item.watchedAt > 0) {
                    Spacer(Modifier.height(4.dp))
                    Text(
                        formatWatchedAt(item.watchedAt),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                }
            }
            IconButton(onClick = onRemove) {
                Icon(
                    Icons.Filled.Delete,
                    contentDescription = "删除",
                    tint = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
    }
}

private val timeFormat = SimpleDateFormat("yyyy-MM-dd HH:mm", Locale.getDefault())

private fun formatWatchedAt(epoch: Long): String = runCatching {
    timeFormat.format(Date(epoch))
}.getOrDefault("")
