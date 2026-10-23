package com.juku.app.ui.screens.following

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.material3.IconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.juku.app.domain.model.FollowingItem
import com.juku.app.ui.components.EmptyState

private val tabLabels = listOf("在看", "想看", "已看")

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FollowingScreen(
    onOpenDrama: (String) -> Unit,
    viewModel: FollowingViewModel = hiltViewModel()
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    var selectedTab by remember { mutableIntStateOf(0) }

    Scaffold(
        topBar = {
            TopAppBar(title = { Text("追剧") })
        }
    ) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding)
        ) {
            TabRow(selectedTabIndex = selectedTab) {
                tabLabels.forEachIndexed { index, label ->
                    Tab(
                        selected = selectedTab == index,
                        onClick = { selectedTab = index },
                        text = { Text(label) }
                    )
                }
            }
            val list = when (selectedTab) {
                0 -> state.watching
                1 -> state.want
                else -> state.watched
            }
            if (list.isEmpty()) {
                EmptyState(message = if (state.loading) "加载中…" else "暂无追剧，去剧库逛逛吧")
            } else {
                LazyColumn(
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(12.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp)
                ) {
                    items(list, key = { it.dramaId }) { item ->
                        FollowingCard(
                            item = item,
                            history = state.historyByDrama[item.dramaId],
                            onClick = {
                                if (item.newEpisodes > 0) viewModel.acknowledge(item.dramaId)
                                onOpenDrama(item.dramaId)
                            }
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun FollowingCard(
    item: FollowingItem,
    history: com.juku.app.domain.model.HistoryItem?,
    onClick: () -> Unit
) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
    ) {
        Row(
            Modifier.padding(14.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(Modifier.weight(1f)) {
                Text(item.title, style = MaterialTheme.typography.titleSmall)
                Spacer(Modifier.padding(2.dp))
                Text(
                    buildString {
                        append("更新至 ${item.knownEpisodes}")
                        if (item.totalEpisode > 0) append("/${item.totalEpisode}")
                        if (item.category?.isNotBlank() == true) append(" · ${item.category}")
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                if (history != null) {
                    Spacer(Modifier.padding(2.dp))
                    Text(
                        (if (history.completed) "看到" else "上次") +
                            " 第 ${history.episode} 集 · ${com.juku.app.util.formatDurationSeconds(history.position)}" +
                            (if (history.duration > 0) " / ${com.juku.app.util.formatDurationSeconds(history.duration)}" else ""),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.primary
                    )
                }
            }
            if (item.newEpisodes > 0) {
                Surface(
                    color = MaterialTheme.colorScheme.error,
                    contentColor = MaterialTheme.colorScheme.onError,
                    shape = MaterialTheme.shapes.small
                ) {
                    Text(
                        "新 ${item.newEpisodes}",
                        style = MaterialTheme.typography.labelSmall,
                        modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp)
                    )
                }
                Spacer(Modifier.width(8.dp))
            }
            IconButton(onClick = onClick) {
                Icon(Icons.Filled.Close, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}
