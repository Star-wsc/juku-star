package com.juku.app.ui.screens.player

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.juku.app.ui.components.episodeGridColumns
import com.juku.app.domain.model.Episode

/** 底部抽屉式分集列表：点 [选集] 按钮弹出，非常驻 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EpisodeDrawer(
    episodes: List<Episode>,
    currentEpisode: Int,
    onSelect: (Int) -> Unit,
    onDismiss: () -> Unit
) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp)
        ) {
            Text("选集", style = MaterialTheme.typography.titleLarge)
            Spacer(Modifier.height(8.dp))
            LazyVerticalGrid(
                columns = episodeGridColumns(),
                contentPadding = PaddingValues(bottom = 32.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .height(380.dp)
            ) {
                items(episodes, key = { it.episode }) { ep ->
                    FilterChip(
                        selected = ep.episode == currentEpisode,
                        onClick = {
                            onDismiss()
                            onSelect(ep.episode)
                        },
                        label = { Text(ep.title.ifBlank { "${ep.episode}" }) },
                        colors = FilterChipDefaults.filterChipColors(
                            selectedContainerColor = MaterialTheme.colorScheme.primaryContainer
                        ),
                        modifier = Modifier.padding(4.dp)
                    )
                }
            }
        }
    }
}
