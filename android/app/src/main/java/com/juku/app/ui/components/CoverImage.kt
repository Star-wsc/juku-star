package com.juku.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil.compose.AsyncImage
import coil.request.ImageRequest

/**
 * iOS 风格海报底色：封面未加载/失败时用稳定渐变色 + 剧名压字，避免灰块闪烁。
 * 配色与 iOS 体验版 PALETTES 对齐，按标题/ID 哈希稳定取色。
 */
data class PosterPalette(val c1: Color, val c2: Color, val c3: Color)

private val PosterPalettes = listOf(
    PosterPalette(Color(0xFF1B3A6B), Color(0xFF4D9FFF), Color(0xFF7CC4FF)),
    PosterPalette(Color(0xFF5C1A1A), Color(0xFFE85D4C), Color(0xFFFF8A65)),
    PosterPalette(Color(0xFF0D3B34), Color(0xFF2BB673), Color(0xFF7DFFB3)),
    PosterPalette(Color(0xFF4A1942), Color(0xFFC86FCB), Color(0xFFFF8AD8)),
    PosterPalette(Color(0xFF3D2914), Color(0xFFE09F3E), Color(0xFFFFE169)),
    PosterPalette(Color(0xFF1A1A2E), Color(0xFF4A4A8A), Color(0xFF8B8BFF)),
    PosterPalette(Color(0xFF3B0F1E), Color(0xFFD6455E), Color(0xFFFF8FA3)),
    PosterPalette(Color(0xFF0F2C4C), Color(0xFF1A73E8), Color(0xFF64B5F6)),
    PosterPalette(Color(0xFF2A1F5E), Color(0xFF6C5CE7), Color(0xFFA29BFE)),
    PosterPalette(Color(0xFF1F3D2B), Color(0xFF52B788), Color(0xFFB7E4C7)),
    PosterPalette(Color(0xFF4A2C0A), Color(0xFFF4A261), Color(0xFFFFE0B2)),
    PosterPalette(Color(0xFF2B0F2E), Color(0xFF9B5DE5), Color(0xFFF15BB5)),
)

fun posterPalette(seed: String): PosterPalette {
    val idx = (seed.hashCode() % PosterPalettes.size + PosterPalettes.size) % PosterPalettes.size
    return PosterPalettes[idx]
}

/**
 * 封面图：渐变占位始终垫底，真图 crossfade 盖上；失败/加载中不露灰块。
 */
@Composable
fun CoverImage(
    url: String?,
    title: String,
    modifier: Modifier = Modifier,
    contentDescription: String? = title,
    showTitleOverlay: Boolean = true
) {
    val seed = title.ifBlank { url.orEmpty() }
    val palette = remember(seed) { posterPalette(seed) }

    Box(modifier = modifier.drawBehind {
        // 对角渐变（对应 iOS 155deg）：必须用 draw 尺寸，不能写死像素
        drawRect(
            Brush.linearGradient(
                colors = listOf(palette.c1, palette.c2, palette.c3),
                start = Offset(0f, 0f),
                end = Offset(size.width, size.height)
            )
        )
    }) {
        if (showTitleOverlay && title.isNotBlank()) {
            Text(
                text = title,
                color = Color.White,
                style = MaterialTheme.typography.bodyMedium.copy(
                    fontWeight = FontWeight.ExtraBold,
                    shadow = Shadow(
                        color = Color.Black.copy(alpha = 0.55f),
                        offset = Offset(0f, 2f),
                        blurRadius = 6f
                    )
                ),
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier
                    .align(Alignment.BottomStart)
                    .padding(8.dp)
            )
        }
        val context = LocalContext.current
        val hasUrl = !url.isNullOrBlank()
        if (!hasUrl && !showTitleOverlay) {
            Text(
                text = "暂无封面",
                color = Color.White.copy(alpha = 0.75f),
                style = MaterialTheme.typography.labelSmall,
                modifier = Modifier.align(Alignment.Center)
            )
        }
        AsyncImage(
            model = ImageRequest.Builder(context)
                .data(url?.takeIf { it.isNotBlank() })
                .crossfade(true)
                .build(),
            contentDescription = contentDescription,
            contentScale = ContentScale.Crop,
            modifier = Modifier.fillMaxSize()
        )
        if (!hasUrl && showTitleOverlay) {
            Text(
                text = "暂无封面",
                color = Color.White.copy(alpha = 0.55f),
                style = MaterialTheme.typography.labelSmall,
                modifier = Modifier.align(Alignment.TopEnd).padding(6.dp)
            )
        }
    }
}
