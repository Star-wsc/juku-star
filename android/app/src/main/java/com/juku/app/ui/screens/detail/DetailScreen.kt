package com.juku.app.ui.screens.detail

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.juku.app.domain.model.Drama
import com.juku.app.domain.model.Episode
import com.juku.app.domain.model.FollowState
import com.juku.app.ui.components.ErrorState
import com.juku.app.ui.components.LoadingState
import com.juku.app.ui.components.releaseStatusLabel
import com.juku.app.ui.components.sourceLabel
import com.juku.app.util.formatDurationSeconds

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun DetailScreen(
    onBack: () -> Unit,
    onPlay: (dramaId: String, episode: Int?, offline: Boolean) -> Unit,
    viewModel: DetailViewModel = hiltViewModel()
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    var showDownloadSheet by remember { mutableStateOf(false) }

    // 打开下载面板时才拉画质，避免进详情就占带宽
    LaunchedEffect(showDownloadSheet) {
        if (showDownloadSheet) viewModel.ensureQualities()
    }

    val snackbarHostState = remember { androidx.compose.material3.SnackbarHostState() }
    LaunchedEffect(state.message) {
        val msg = state.message
        if (msg != null) {
            snackbarHostState.showSnackbar(msg)
            viewModel.consumeMessage()
        }
    }

    Scaffold(
        snackbarHost = { androidx.compose.material3.SnackbarHost(snackbarHostState) },
        topBar = {
            TopAppBar(
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
                title = {
                    Text(
                        state.drama?.title ?: "详情",
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                }
            )
        }
    ) { padding ->
        when {
            state.loading -> LoadingState(Modifier.padding(padding))
            state.drama == null -> ErrorState(
                message = state.error ?: "获取剧集信息失败",
                onRetry = viewModel::retry,
                modifier = Modifier.padding(padding)
            )
            else -> {
                val drama = state.drama!!
                LazyColumn(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(padding),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp)
                ) {
                    item { DramaHeader(drama) }
                    item {
                        Text(
                            drama.desc.ifBlank { "暂无简介" },
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                    }
                    if (drama.tags.isNotEmpty()) {
                        item {
                            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                drama.tags.forEach { tag ->
                                    AssistChip(onClick = {}, label = { Text(tag) })
                                }
                            }
                        }
                    }
                    item {
                        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                            FollowButton(
                                state = state.followState,
                                busy = state.followBusy,
                                onFollow = viewModel::follow,
                                onWatched = viewModel::markWatched,
                                onUnfollow = viewModel::unfollow
                            )
                            val history = state.history
                            // 播放过就显示「继续播放」（含当前集已看完、整部未看完）
                            val canResume = history != null
                            Button(
                                onClick = { onPlay(drama.id, null, false) },
                                modifier = Modifier.weight(1f)
                            ) {
                                Icon(Icons.Filled.PlayArrow, contentDescription = null)
                                Spacer(Modifier.width(4.dp))
                                Text(if (canResume) "继续播放" else "播放")
                            }
                            OutlinedButton(
                                onClick = { showDownloadSheet = true },
                                modifier = Modifier.weight(1f)
                            ) {
                                Text("下载")
                            }
                        }
                    }
                    if (state.history != null) {
                        item {
                            val h = state.history!!
                            val progress = if (h.duration > 0) (h.position / h.duration).toFloat().coerceIn(0f, 1f) else 0f
                            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                Text(
                                    (if (h.completed) "上次看完" else "上次看到") +
                                        " 第 ${h.episode} 集 · ${formatDurationSeconds(h.position)}" +
                                        (if (h.duration > 0) " / ${formatDurationSeconds(h.duration)}" else ""),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant
                                )
                                if (h.duration > 0) {
                                    LinearProgressIndicator(
                                        progress = { progress },
                                        modifier = Modifier.fillMaxWidth()
                                    )
                                }
                            }
                        }
                    }
                    if (state.downloaded.isNotEmpty()) {
                        item {
                            val history = state.history
                            val canOfflineResume = history != null && !history.completed &&
                                history.episode in state.downloaded.keys
                            OutlinedButton(
                                // 不指定集数，由播放器按历史进度续播
                                onClick = { onPlay(drama.id, null, true) },
                                modifier = Modifier.fillMaxWidth()
                            ) {
                                Text(
                                    if (canOfflineResume) "离线续播（第 ${history!!.episode} 集）"
                                    else "离线播放（已下载 ${state.downloaded.size} 集）"
                                )
                            }
                        }
                    }
                    if (state.episodes.isNotEmpty()) {
                        item {
                            Text(
                                "分集 · ${state.episodes.size}",
                                style = MaterialTheme.typography.titleMedium
                            )
                        }
                        item {
                            FlowRow(
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                                verticalArrangement = Arrangement.spacedBy(8.dp)
                            ) {
                                state.episodes.forEach { episode ->
                                    val downloaded = state.downloaded.containsKey(episode.episode)
                                    AssistChip(
                                        onClick = { onPlay(drama.id, episode.episode, false) },
                                        label = { Text(episode.title.ifBlank { "${episode.episode}" }) },
                                        leadingIcon = if (downloaded) {
                                            {
                                                Icon(
                                                    Icons.Filled.Check,
                                                    contentDescription = "已下载",
                                                    modifier = Modifier.size(14.dp),
                                                    tint = MaterialTheme.colorScheme.primary
                                                )
                                            }
                                        } else null
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    if (showDownloadSheet && state.drama != null) {
        DownloadSheet(
            episodes = state.episodes,
            qualities = state.qualities,
            defaultQuality = state.defaultDownloadQuality,
            downloadedEpisodes = state.downloaded.keys,
            onDismiss = { showDownloadSheet = false },
            onConfirm = { eps, quality ->
                showDownloadSheet = false
                viewModel.download(eps, quality)
            }
        )
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun DramaHeader(drama: Drama) {
    Row {
        Box(
            modifier = Modifier
                .width(110.dp)
                .aspectRatio(2f / 3f)
                .clip(RoundedCornerShape(10.dp))
        ) {
            com.juku.app.ui.components.CoverImage(
                url = drama.coverUrl,
                title = drama.title,
                modifier = Modifier.fillMaxSize()
            )
        }
        Spacer(Modifier.width(14.dp))
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(
                drama.title,
                style = MaterialTheme.typography.titleLarge,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(
                    sourceLabel(drama.source),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary
                )
                if (drama.category.isNotBlank()) {
                    Text(drama.category, style = MaterialTheme.typography.labelMedium)
                }
                releaseStatusLabel(drama.releaseStatus).takeIf { it.isNotBlank() }?.let {
                    Text(it, style = MaterialTheme.typography.labelMedium)
                }
                if (drama.vip) {
                    Surface(color = Color(0xFFF2A600), contentColor = Color.Black, shape = RoundedCornerShape(4.dp)) {
                        Text(
                            "VIP",
                            style = MaterialTheme.typography.labelSmall,
                            modifier = Modifier.padding(horizontal = 4.dp)
                        )
                    }
                }
            }
            Text(
                "共 ${drama.totalEpisode} 集" +
                    (if (drama.score.isNotBlank()) " · 评分 ${drama.score}" else "") +
                    (if (drama.views.isNotBlank()) " · ${drama.views}播放" else ""),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            if (drama.onlineDate.isNotBlank()) {
                Text(
                    "上线日期：${drama.onlineDate}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun FollowButton(
    state: FollowState,
    busy: Boolean,
    onFollow: () -> Unit,
    onWatched: () -> Unit,
    onUnfollow: () -> Unit
) {
    var menuOpen by remember { mutableStateOf(false) }
    val label = when (state) {
        FollowState.NONE -> "追剧"
        FollowState.WANT -> "想看"
        FollowState.WATCHING -> "在看"
        FollowState.WATCHED -> "已看"
    }
    Box {
        OutlinedButton(onClick = { menuOpen = true }, enabled = !busy) {
            if (state != FollowState.NONE) {
                Icon(
                    Icons.Filled.Favorite,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                    tint = MaterialTheme.colorScheme.primary
                )
                Spacer(Modifier.width(4.dp))
            }
            Text(label)
        }
        androidx.compose.material3.DropdownMenu(
            expanded = menuOpen,
            onDismissRequest = { menuOpen = false }
        ) {
            androidx.compose.material3.DropdownMenuItem(
                text = { Text("想看") },
                onClick = {
                    menuOpen = false
                    onFollow()
                }
            )
            androidx.compose.material3.DropdownMenuItem(
                text = { Text("标记已看") },
                onClick = {
                    menuOpen = false
                    onWatched()
                }
            )
            if (state != FollowState.NONE) {
                androidx.compose.material3.DropdownMenuItem(
                    text = { Text("取消追剧") },
                    onClick = {
                        menuOpen = false
                        onUnfollow()
                    }
                )
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class, ExperimentalMaterial3Api::class)
@Composable
private fun DownloadSheet(
    episodes: List<Episode>,
    qualities: List<com.juku.app.domain.model.QualityOption>?,
    defaultQuality: Int,
    downloadedEpisodes: Set<Int>,
    onDismiss: () -> Unit,
    onConfirm: (episodes: List<Int>, quality: Int) -> Unit
) {
    var selectedQuality by remember(qualities, defaultQuality) {
        mutableStateOf(
            if (defaultQuality != Int.MIN_VALUE && qualities?.any { it.value == defaultQuality } == true) {
                defaultQuality
            } else {
                qualities?.firstOrNull()?.value ?: Int.MIN_VALUE
            }
        )
    }
    var selected by remember { mutableStateOf<Set<Int>>(emptySet()) }
    val downloadable = episodes.filter { it.episode !in downloadedEpisodes }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 16.dp)) {
            Text("下载", style = MaterialTheme.typography.titleLarge)
            Spacer(Modifier.height(12.dp))
            Text("画质", style = MaterialTheme.typography.titleSmall)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (qualities.isNullOrEmpty()) {
                    FilterChip(
                        selected = true,
                        onClick = {},
                        label = { Text("默认画质") }
                    )
                } else {
                    qualities.forEach { q ->
                        FilterChip(
                            selected = selectedQuality == q.value,
                            onClick = { selectedQuality = q.value },
                            label = { Text(q.label) }
                        )
                    }
                }
            }
            Spacer(Modifier.height(12.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("分集（${selected.size}/${downloadable.size}）", style = MaterialTheme.typography.titleSmall)
                Spacer(Modifier.weight(1f))
                TextButton(onClick = { selected = downloadable.map { it.episode }.toSet() }) { Text("全选") }
                TextButton(onClick = { selected = emptySet() }) { Text("清除") }
            }
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.height(280.dp)
            ) {
                episodes.forEach { ep ->
                    val isDownloaded = ep.episode in downloadedEpisodes
                    FilterChip(
                        selected = ep.episode in selected,
                        onClick = {
                            if (!isDownloaded) {
                                selected = if (ep.episode in selected) selected - ep.episode
                                else selected + ep.episode
                            }
                        },
                        enabled = !isDownloaded,
                        label = { Text("${ep.episode}") },
                        leadingIcon = if (isDownloaded) {
                            { Icon(Icons.Filled.Check, contentDescription = null, modifier = Modifier.size(14.dp)) }
                        } else null
                    )
                }
            }
            Spacer(Modifier.height(12.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onDismiss) { Text("取消") }
                Spacer(Modifier.width(8.dp))
                Button(
                    onClick = { onConfirm(selected.toList().sorted(), selectedQuality) },
                    enabled = selected.isNotEmpty()
                ) {
                    Text("开始下载（${selected.size} 集）")
                }
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}
