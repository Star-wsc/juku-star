package com.juku.app.ui.screens.search

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.juku.app.ui.components.DramaCard
import com.juku.app.ui.components.EmptyState
import com.juku.app.ui.components.LoadingState
import com.juku.app.ui.components.posterGridColumns

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SearchScreen(
    onBack: () -> Unit,
    onOpenDrama: (String) -> Unit,
    viewModel: SearchViewModel = hiltViewModel()
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val focusRequester = remember { FocusRequester() }

    LaunchedEffect(Unit) {
        focusRequester.requestFocus()
    }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
                title = { Text("搜索") }
            )
        }
    ) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding)
        ) {
            androidx.compose.material3.OutlinedTextField(
                value = state.query,
                onValueChange = viewModel::onQueryChange,
                placeholder = { Text("搜索剧名") },
                singleLine = true,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 8.dp)
                    .focusRequester(focusRequester)
            )
            when {
                state.query.isBlank() -> EmptyState(message = "输入剧名开始搜索")
                state.searching && state.onlineResults.isEmpty() && state.localResults.isEmpty() && state.suggestions.isEmpty() ->
                    LoadingState(message = "搜索中…")
                state.localResults.isEmpty() && state.onlineResults.isEmpty() && state.suggestions.isEmpty() && state.error == null ->
                    EmptyState(message = "没有找到相关剧集")
                else -> {
                    LazyVerticalGrid(
                        columns = posterGridColumns(),
                        contentPadding = PaddingValues(12.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                        modifier = Modifier.fillMaxSize()
                    ) {
                        if (state.suggestions.isNotEmpty()) {
                            item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(3) }) {
                                Text(
                                    "搜索联想",
                                    style = MaterialTheme.typography.titleSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant
                                )
                            }
                            items(state.suggestions, key = { "sug_$it" }) { text ->
                                androidx.compose.material3.SuggestionChip(
                                    onClick = { viewModel.onSuggestionClick(text) },
                                    label = { Text(text) },
                                    modifier = Modifier.fillMaxWidth()
                                )
                            }
                        }
                        if (state.localResults.isNotEmpty()) {
                            item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(3) }) {
                                Text(
                                    "本地结果 ${state.localResults.size}",
                                    style = MaterialTheme.typography.titleSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant
                                )
                            }
                            items(state.localResults, key = { "local_${it.id}" }) { drama ->
                                DramaCard(drama, onClick = { onOpenDrama(drama.id) })
                            }
                        }
                        if (state.onlineResults.isNotEmpty()) {
                            item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(3) }) {
                                Text(
                                    "联网搜索 ${state.onlineResults.size}",
                                    style = MaterialTheme.typography.titleSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant
                                )
                            }
                            items(state.onlineResults, key = { "online_${it.id}" }) { drama ->
                                DramaCard(drama, onClick = { onOpenDrama(drama.id) })
                            }
                        }
                    }
                    if (state.error != null) {
                        Text(
                            state.error ?: "",
                            color = MaterialTheme.colorScheme.error,
                            style = MaterialTheme.typography.bodySmall,
                            modifier = Modifier.padding(16.dp)
                        )
                    }
                }
            }
        }
    }
}
