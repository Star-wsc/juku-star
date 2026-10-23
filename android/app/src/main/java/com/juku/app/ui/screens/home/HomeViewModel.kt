package com.juku.app.ui.screens.home

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.data.repository.DramaRepositoryImpl
import com.juku.app.domain.model.Drama
import com.juku.app.domain.model.RecommendGenre
import com.juku.app.domain.repository.AuthRepository
import com.juku.app.util.parseViewCount
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

enum class SortOption(val label: String) {
    DEFAULT("最新上线"),
    NAME("名称"),
    DATE("上线日期"),
    VIEWS("播放量"),
    /** 随机乱序；选中其它排序会自动退出随机 */
    SHUFFLE("随机")
}

/** 站源排序优先级：红果(hongguo)第一，其余按字母排 */
private val SOURCE_ORDER = listOf("hongguo", "huangguo", "huangguo-video", "huangdou", "cloudfront")

/**
 * 将子源归入站源组，用于 chip 筛选匹配。
 * 真实数据里其他源A有多个子源（huangguo / huangguoai / huangguo-video / cloudfront），
 * 其他源B同理（huangdou / huangdouai 等），chip 标签只显示组名。
 */
private fun sourceGroup(source: String?): String = when {
    source == null -> ""
    source.startsWith("hongguo") -> "hongguo"
    source.startsWith("huangguo") || source == "cloudfront" -> "huangguo"
    source.startsWith("huangdou") -> "huangdou"
    else -> source
}

private fun sortSources(sources: List<String>): List<String> =
    sources.sortedBy { src ->
        val idx = SOURCE_ORDER.indexOf(src)
        if (idx >= 0) idx else SOURCE_ORDER.size + src.hashCode()
    }

/** 与 WEB 一致：分类名按 zh-Hans-CN 排序 */
private fun sortCategories(categories: List<String>): List<String> =
    categories.sortedWith(java.text.Collator.getInstance(java.util.Locale.SIMPLIFIED_CHINESE))

/** 红果分类推荐（与 WEB recommendGenre 一致） */
val RECOMMEND_GENRES = listOf(
    RecommendGenre("short_play", "真人剧"),
    RecommendGenre("comic_series", "漫剧"),
    RecommendGenre("ai_series", "AI 剧")
)


/** 最新上线优先：有日期的按日期降序，无日期的排在后面，同日期按标题稳定排序 */
private fun newestOnlineFirst(list: List<Drama>): List<Drama> =
    list.sortedWith(
        compareByDescending<Drama> { it.onlineDate.isNotBlank() }
            .thenByDescending { it.onlineDate }
            .thenBy { it.title }
    )

data class HomeUiState(
    val loading: Boolean = true,
    val refreshing: Boolean = false,
    val loadingMore: Boolean = false,
    val error: String? = null,
    val dramas: List<Drama> = emptyList(),
    val more: Boolean = false,
    val revision: Long = 0,
    val sources: List<String> = emptyList(),
    val selectedSource: String? = "hongguo", // 默认打开选中红果标签
    val selectedCategory: String? = null,    // null = 全部分类（本地筛选）
    val categories: List<String> = emptyList(),
    val selectedGenre: String? = null,       // 分类推荐：short_play | comic_series | ai_series
    val recommendItems: List<Drama> = emptyList(),
    val recommendHasMore: Boolean = false,
    val recommendBusy: Boolean = false,
    val recommendError: String? = null,
    val sort: SortOption = SortOption.SHUFFLE,
    /** 随机排序种子；sort==SHUFFLE 时生效，换种子即重新洗牌 */
    val shuffleSeed: Long = 0L,
    val hideVip: Boolean = false,
    val showHuangguo: Boolean = true,
    val showHuangdou: Boolean = true
) {
    /** 当前可见剧集（站源开关 + 选中站源），分类列表与筛选都从它派生 */
    private val sourceFiltered: List<Drama>
        get() {
            var list = dramas.filter { !AppPrefs.isSourceHiddenFor(showHuangguo, showHuangdou, it.source) }
            if (selectedSource != null) {
                // 按站源组匹配：选"其他源A"时包含 huangguo / huangguoai / huangguo-video / cloudfront 等子源
                list = list.filter { sourceGroup(it.source) == selectedSource || it.source == selectedSource }
            }
            return list.distinctBy { it.id }
        }

    /** 按站源开关 + 选中站源 + 分类 + VIP过滤 + 排序选项，返回要显示的剧集 */
    val filtered: List<Drama>
        get() {
            var list = sourceFiltered
            val category = selectedCategory
            if (category != null) {
                list = list.filter { it.category.trim() == category || category in it.tags }
            }
            if (hideVip) list = list.filter { !it.vip }
            return when (sort) {
                SortOption.SHUFFLE -> list.shuffled(java.util.Random(shuffleSeed))
                // 默认：最新上线在前，每次打开新剧优先
                SortOption.DEFAULT, SortOption.DATE -> newestOnlineFirst(list)
                SortOption.NAME -> list.sortedBy { it.title }
                SortOption.VIEWS -> list.sortedByDescending { parseViewCount(it.views) }
            }
        }

    /** 由 dramas/selectedSource 变化时重算，避免组合期反复 new List */
    fun withDerived(): HomeUiState {
        val cats = sourceFiltered
            .map { it.category.trim() }
            .filter { it.isNotEmpty() }
            .distinct()
            .let(::sortCategories)
        val category = selectedCategory?.takeIf { it in cats }
        return copy(categories = cats, selectedCategory = category)
    }
}

@HiltViewModel
class HomeViewModel @Inject constructor(
    private val prefs: AppPrefs,
    private val dramaRepository: DramaRepositoryImpl,
    authRepository: AuthRepository
) : ViewModel() {

    private val _state = MutableStateFlow(HomeUiState())
    val state: StateFlow<HomeUiState> = _state.asStateFlow()

    private var lastViewerId: String? = null

    // 分类推荐分页游标（按 genre 分别保存）
    private data class RecommendCursor(
        var offset: Int = 0,
        var sessionId: String = "",
        val seen: MutableList<String> = mutableListOf()
    )

    private val recommendCursors = mutableMapOf<String, RecommendCursor>()

    init {
        val savedSort = runCatching { SortOption.valueOf(prefs.homeSort) }.getOrDefault(SortOption.SHUFFLE)
        _state.update {
            it.copy(
                sort = savedSort,
                // 进随机模式先洗一次牌
                shuffleSeed = if (savedSort == SortOption.SHUFFLE) java.util.Random().nextLong() else 0L
            )
        }
        viewModelScope.launch {
            combine(
                authRepository.viewerState,
                prefs.showHuangguoState,
                prefs.showHuangdouState
            ) { viewer, showHuangguo, showHuangdou ->
                Triple(viewer, showHuangguo, showHuangdou)
            }.collect { (viewer, showHuangguo, showHuangdou) ->
                val visible = (viewer?.sources ?: emptyList())
                    .filter { !AppPrefs.isSourceHiddenFor(showHuangguo, showHuangdou, it) }
                    // 红果永远不会被 isSourceHiddenFor 隐藏；若 viewer.sources 未列出也补上
                    .let { list ->
                        if ("hongguo" !in list &&
                            !AppPrefs.isSourceHiddenFor(showHuangguo, showHuangdou, "hongguo")
                        ) list + "hongguo" else list
                    }
                    .let(::sortSources)
                _state.update {
                    val fallbackSelection = if (visible.isNotEmpty()) {
                        // 选中的站源被屏蔽时回退；未被屏蔽但不在列表时也保留（防误清）
                        it.selectedSource?.takeIf { sel ->
                            visible.contains(sel) ||
                                !AppPrefs.isSourceHiddenFor(showHuangguo, showHuangdou, sel)
                        }
                    } else {
                        it.selectedSource
                    }
                    it.copy(
                        sources = visible,
                        showHuangguo = showHuangguo,
                        showHuangdou = showHuangdou,
                        selectedSource = fallbackSelection
                    ).withDerived()
                }
                // 服务器地址变更（viewer 身份变化）后强制刷新列表
                val viewerId = viewer?.id
                val serverSwitched = lastViewerId != null && viewerId != lastViewerId
                lastViewerId = viewerId
                if (serverSwitched) {
                    load()
                } else if (viewer != null && _state.value.dramas.isEmpty() &&
                    !_state.value.loading && !_state.value.refreshing
                ) {
                    // 登录成功且当前列表为空（此前 401）时自动重载
                    load()
                }
            }
        }
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = it.dramas.isEmpty(), error = null) }
            try {
                val page = dramaRepository.getDramas(refresh = false)
                _state.update {
                    it.copy(
                        loading = false,
                        dramas = page.dramas,
                        more = page.more,
                        revision = page.revision
                    ).withDerived()
                }
            } catch (e: Exception) {
                _state.update { it.copy(loading = false, error = e.message ?: "加载失败") }
            }
        }
    }

    fun refresh() {
        if (_state.value.refreshing) return
        viewModelScope.launch {
            val newSeed = if (_state.value.sort == SortOption.SHUFFLE) java.util.Random().nextLong() else _state.value.shuffleSeed
            _state.update { it.copy(refreshing = true, error = null, shuffleSeed = newSeed) }
            try {
                // 普通刷新：只取服务端快照，不触发源站拉取
                val page = dramaRepository.reloadSnapshot()
                _state.update {
                    it.copy(
                        refreshing = false,
                        dramas = page.dramas,
                        more = page.more,
                        revision = page.revision
                    ).withDerived()
                }
            } catch (e: Exception) {
                _state.update { it.copy(refreshing = false, error = e.message ?: "刷新失败") }
            }
        }
    }


    fun loadMore() {
        val current = _state.value
        if (current.loadingMore || !current.more || current.loading || current.refreshing) return
        viewModelScope.launch {
            _state.update { it.copy(loadingMore = true) }
            try {
                val page = dramaRepository.loadMore(current.revision)
                _state.update {
                    // distinctBy：loadMore 追加可能与已有 id 重复，LazyGrid key 重复会崩
                    it.copy(
                        loadingMore = false,
                        dramas = (current.dramas + page.dramas).distinctBy { d -> d.id },
                        more = page.more,
                        revision = page.revision
                    ).withDerived()
                }
            } catch (_: Exception) {
                _state.update { it.copy(loadingMore = false) }
            }
        }
    }

    fun selectSource(source: String?) = _state.update {
        // 切站源后分类集合会变，清空分类避免筛空；随机模式下换筛选就重新洗牌
        val reseed = if (it.sort == SortOption.SHUFFLE) java.util.Random().nextLong() else it.shuffleSeed
        it.copy(
            selectedSource = source,
            selectedCategory = null,
            shuffleSeed = reseed
        ).withDerived()
    }

    fun selectCategory(category: String?) = _state.update {
        val reseed = if (it.sort == SortOption.SHUFFLE) java.util.Random().nextLong() else it.shuffleSeed
        it.copy(
            selectedCategory = category?.takeIf { c -> c in it.categories || c.isEmpty() },
            shuffleSeed = reseed
        )
    }
    fun selectSort(sort: SortOption) {
        prefs.homeSort = sort.name
        _state.update {
            it.copy(
                sort = sort,
                // 选「随机」立刻洗牌；选其它排序自动退出随机（sort 换掉即可）
                shuffleSeed = if (sort == SortOption.SHUFFLE) java.util.Random().nextLong() else 0L
            )
        }
    }
    fun toggleHideVip() = _state.update {
        val reseed = if (it.sort == SortOption.SHUFFLE) java.util.Random().nextLong() else it.shuffleSeed
        it.copy(hideVip = !it.hideVip, shuffleSeed = reseed)
    }

    /** 选择红果分类推荐：真人剧 / 漫剧 / AI 剧；再点同一项退出推荐模式 */
    fun selectGenre(genreId: String?) {
        if (genreId != null && _state.value.sort == SortOption.SHUFFLE) {
            _state.update { it.copy(shuffleSeed = java.util.Random().nextLong()) }
        }
        val current = _state.value.selectedGenre
        if (genreId == null || genreId == current) {
            _state.update {
                it.copy(
                    selectedGenre = null,
                    recommendItems = emptyList(),
                    recommendHasMore = false,
                    recommendError = null,
                    recommendBusy = false
                )
            }
            return
        }
        val cached = recommendCursors[genreId]
        _state.update {
            it.copy(
                selectedGenre = genreId,
                recommendItems = emptyList(),
                recommendHasMore = true,
                recommendError = null,
                recommendBusy = true
            )
        }
        loadRecommendations(genreId, reset = cached == null)
    }

    fun loadMoreRecommendations() {
        val s = _state.value
        val genre = s.selectedGenre ?: return
        if (s.recommendBusy || !s.recommendHasMore) return
        loadRecommendations(genre, reset = false)
    }

    private fun loadRecommendations(genre: String, reset: Boolean) {
        viewModelScope.launch {
            _state.update { it.copy(recommendBusy = true, recommendError = null) }
            try {
                if (reset) recommendCursors.remove(genre)
                val cursor = recommendCursors.getOrPut(genre) { RecommendCursor() }
                val page = dramaRepository.getRecommendations(
                    genre = genre,
                    offset = if (reset) 0 else cursor.offset,
                    sessionId = if (reset) "" else cursor.sessionId,
                    seen = if (reset) emptyList() else cursor.seen.toList()
                )
                cursor.offset = page.nextOffset
                cursor.sessionId = page.sessionId
                page.dramas.forEach { if (it.id !in cursor.seen) cursor.seen.add(it.id) }
                _state.update { s ->
                    if (s.selectedGenre != genre) s
                    else s.copy(
                        recommendBusy = false,
                        recommendItems = (if (reset) page.dramas else s.recommendItems + page.dramas)
                            .distinctBy { d -> d.id },
                        recommendHasMore = page.hasMore,
                        recommendError = null
                    )
                }
            } catch (e: Exception) {
                _state.update { s ->
                    if (s.selectedGenre != genre) s
                    else s.copy(recommendBusy = false, recommendError = e.message ?: "获取推荐失败")
                }
            }
        }
    }
}
