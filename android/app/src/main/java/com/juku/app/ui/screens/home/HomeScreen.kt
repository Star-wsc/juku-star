package com.juku.app.ui.screens.home

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Sort
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.juku.app.domain.model.Drama
import com.juku.app.domain.model.RecommendGenre
import com.juku.app.ui.components.DramaCard
import com.juku.app.ui.components.EmptyState
import com.juku.app.ui.components.ErrorState
import com.juku.app.ui.components.LoadingState
import com.juku.app.ui.components.posterGridColumns

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(
    onOpenDrama: (String) -> Unit,
    onOpenSearch: () -> Unit,
    viewModel: HomeViewModel = hiltViewModel()
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val gridState = rememberLazyGridState()

    val shouldLoadMore by remember {
        derivedStateOf {
            val info = gridState.layoutInfo
            val lastVisible = info.visibleItemsInfo.lastOrNull()?.index ?: 0
            info.totalItemsCount > 0 && lastVisible >= info.totalItemsCount - 4
        }
    }
    LaunchedEffect(shouldLoadMore, state.more, state.selectedGenre, state.recommendHasMore) {
        if (!shouldLoadMore) return@LaunchedEffect
        if (state.selectedGenre != null) {
            if (state.recommendHasMore) viewModel.loadMoreRecommendations()
        } else if (state.more) {
            viewModel.loadMore()
        }
    }

    // 筛选变化后列表长度会突变，必须回顶，否则 LazyGrid 会因旧锚点越界闪退
    LaunchedEffect(state.selectedSource, state.selectedCategory, state.selectedGenre, state.hideVip, state.shuffleSeed) {
        gridState.scrollToItem(0)
    }
    // 刷新结束后回顶，避免乱序后停在列表中间
    var wasRefreshing by remember { mutableStateOf(false) }
    LaunchedEffect(state.refreshing) {
        if (wasRefreshing && !state.refreshing) {
            gridState.scrollToItem(0)
        }
        wasRefreshing = state.refreshing
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("短剧库") },
                actions = {
                    IconButton(onClick = onOpenSearch) {
                        Icon(Icons.Filled.Search, contentDescription = "搜索")
                    }
                }
            )
        }
    ) { padding ->
        PullToRefreshBox(
            isRefreshing = state.refreshing,
            onRefresh = viewModel::refresh,
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
        ) {
            when {
                state.loading -> LoadingState()
                state.error != null && state.dramas.isEmpty() -> ErrorState(
                    message = state.error ?: "",
                    onRetry = viewModel::load
                )
                else -> Column(Modifier.fillMaxSize()) {
                    FilterBar(
                        sources = state.sources,
                        selectedSource = state.selectedSource,
                        onSelectSource = viewModel::selectSource,
                        genres = RECOMMEND_GENRES,
                        selectedGenre = state.selectedGenre,
                        onSelectGenre = viewModel::selectGenre,
                        categories = state.categories,
                        selectedCategory = state.selectedCategory,
                        onSelectCategory = viewModel::selectCategory,
                        sort = state.sort,
                        onSelectSort = viewModel::selectSort,
                        hideVip = state.hideVip,
                        onToggleVip = viewModel::toggleHideVip
                    )
                    val recommendMode = state.selectedGenre != null
                    if (recommendMode) {
                        if (state.recommendError != null && state.recommendItems.isEmpty()) {
                            ErrorState(
                                message = state.recommendError ?: "获取推荐失败",
                                onRetry = { viewModel.selectGenre(state.selectedGenre) }
                            )
                        } else if (state.recommendItems.isEmpty() && state.recommendBusy) {
                            LoadingState()
                        } else if (state.recommendItems.isEmpty()) {
                            EmptyState(message = "暂无推荐内容\n可下拉刷新剧库后再试", actionText = "刷新", onAction = viewModel::refresh)
                        } else {
                            val recommendList = remember(
                                state.recommendItems, state.sort, state.shuffleSeed
                            ) {
                                if (state.sort == com.juku.app.ui.screens.home.SortOption.SHUFFLE) {
                                    state.recommendItems.shuffled(java.util.Random(state.shuffleSeed))
                                } else {
                                    state.recommendItems
                                }
                            }
                            LazyVerticalGrid(
                                columns = posterGridColumns(),
                                state = gridState,
                                contentPadding = PaddingValues(12.dp),
                                verticalArrangement = Arrangement.spacedBy(12.dp),
                                horizontalArrangement = Arrangement.spacedBy(12.dp),
                                modifier = Modifier.fillMaxSize()
                            ) {
                                items(recommendList, key = { it.id }) { drama ->
                                    DramaCard(drama = drama, onClick = { onOpenDrama(drama.id) })
                                }
                                if (state.recommendHasMore && state.recommendBusy) {
                                    item(key = "recommend-more") {
                                        Box(
                                            Modifier.fillMaxWidth(),
                                            contentAlignment = Alignment.Center
                                        ) {
                                            LinearProgressIndicator(Modifier.fillMaxWidth(0.4f))
                                        }
                                    }
                                }
                            }
                        }
                    } else if (state.dramas.isEmpty()) {
                        EmptyState(
                            message = "剧库为空\n下拉刷新或点按钮重新加载",
                            actionText = "刷新剧库",
                            onAction = viewModel::refresh
                        )
                    } else {
                        // 记忆化：几千部剧的过滤+排序只在输入变化时执行，
                        // 不随界面状态（刷新/加载更多指示器等）翻转反复计算
                        val dramas = remember(
                            state.dramas, state.selectedSource, state.selectedCategory, state.hideVip,
                            state.sort, state.shuffleSeed, state.showHuangguo, state.showHuangdou
                        ) {
                            state.filtered
                        }
                        if (dramas.isEmpty()) {
                            EmptyState(
                                message = "该分类下暂无剧集\n可清空筛选或下拉刷新",
                                actionText = "清空分类",
                                onAction = { viewModel.selectCategory(null) }
                            )
                        } else {
                            LazyVerticalGrid(
                                columns = posterGridColumns(),
                                state = gridState,
                                contentPadding = PaddingValues(12.dp),
                                verticalArrangement = Arrangement.spacedBy(12.dp),
                                horizontalArrangement = Arrangement.spacedBy(12.dp),
                                modifier = Modifier.fillMaxSize()
                            ) {
                                items(dramas, key = { it.id }) { drama ->
                                    DramaCard(drama = drama, onClick = { onOpenDrama(drama.id) })
                                }
                            }
                        }
                    }
                    if (state.loadingMore) {
                        LinearProgressIndicator(Modifier.fillMaxWidth())
                    }
                }
            }
        }
    }
}

@Composable
private fun FilterBar(
    sources: List<String>,
    selectedSource: String?,
    onSelectSource: (String?) -> Unit,
    genres: List<RecommendGenre>,
    selectedGenre: String?,
    onSelectGenre: (String?) -> Unit,
    categories: List<String>,
    selectedCategory: String?,
    onSelectCategory: (String?) -> Unit,
    sort: SortOption,
    onSelectSort: (SortOption) -> Unit,
    hideVip: Boolean,
    onToggleVip: () -> Unit
) {
    var sortMenuOpen by remember { mutableStateOf(false) }
    var genreMenuOpen by remember { mutableStateOf(false) }
    var categoryMenuOpen by remember { mutableStateOf(false) }

    val genreLabel = genres.firstOrNull { it.id == selectedGenre }?.label ?: "关闭"
    val categoryLabel = selectedCategory ?: "全部"

    Column {
        // 站源：红果 / 其他源A / 其他源B / 全部
        LazyRow(
            contentPadding = PaddingValues(horizontal = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            items(sources) { source ->
                FilterChip(
                    selected = selectedSource == source,
                    onClick = { onSelectSource(if (selectedSource == source) null else source) },
                    label = { Text(sourceLabelOf(source)) }
                )
            }
            item {
                FilterChip(
                    selected = selectedSource == null,
                    onClick = { onSelectSource(null) },
                    label = { Text("全部") }
                )
            }
        }
        // 分类 / 排序 / VIP：与排序同款下拉，限高内部滚动
        LazyRow(
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            item {
                Box {
                    FilterChip(
                        selected = selectedGenre != null,
                        onClick = { genreMenuOpen = true },
                        label = { Text("分类: $genreLabel") }
                    )
                    DropdownMenu(
                        expanded = genreMenuOpen,
                        onDismissRequest = { genreMenuOpen = false },
                        modifier = Modifier.heightIn(max = 220.dp)
                    ) {
                        DropdownMenuItem(
                            text = { Text("关闭") },
                            onClick = {
                                onSelectGenre(null)
                                genreMenuOpen = false
                            }
                        )
                        genres.forEach { genre ->
                            DropdownMenuItem(
                                text = { Text(genre.label) },
                                onClick = {
                                    onSelectGenre(genre.id)
                                    genreMenuOpen = false
                                }
                            )
                        }
                    }
                }
            }
            item {
                Box {
                    FilterChip(
                        selected = selectedCategory != null && selectedGenre == null,
                        enabled = selectedGenre == null,
                        onClick = { categoryMenuOpen = true },
                        label = { Text("剧库: $categoryLabel") }
                    )
                    DropdownMenu(
                        expanded = categoryMenuOpen,
                        onDismissRequest = { categoryMenuOpen = false },
                        modifier = Modifier.heightIn(max = 220.dp)
                    ) {
                        DropdownMenuItem(
                            text = { Text("全部") },
                            onClick = {
                                onSelectCategory(null)
                                categoryMenuOpen = false
                            }
                        )
                        categories.forEach { category ->
                            DropdownMenuItem(
                                text = { Text(category) },
                                onClick = {
                                    onSelectCategory(category)
                                    categoryMenuOpen = false
                                }
                            )
                        }
                    }
                }
            }
            item {
                Box {
                    FilterChip(
                        selected = sort != SortOption.DEFAULT,
                        onClick = { sortMenuOpen = true },
                        label = { Text("排序: ${sort.label}") },
                        leadingIcon = { Icon(Icons.Filled.Sort, contentDescription = null) }
                    )
                    DropdownMenu(
                        expanded = sortMenuOpen,
                        onDismissRequest = { sortMenuOpen = false },
                        modifier = Modifier.heightIn(max = 220.dp)
                    ) {
                        SortOption.entries.forEach { option ->
                            DropdownMenuItem(
                                text = { Text(option.label) },
                                onClick = {
                                    onSelectSort(option)
                                    sortMenuOpen = false
                                }
                            )
                        }
                    }
                }
            }
            item {
                FilterChip(
                    selected = hideVip,
                    onClick = onToggleVip,
                    label = { Text(if (hideVip) "隐藏VIP" else "显示VIP") }
                )
            }
        }
    }
}

private fun sourceLabelOf(source: String): String = com.juku.app.ui.components.sourceDisplayName(source)
