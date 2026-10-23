package com.juku.app.ui.components

import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.unit.dp

/**
 * 海报网格列数：按可用宽度自适应，避免平板/横屏把封面拉爆。
 * 手机竖屏约 3 列；平板横屏 6–8 列，单卡最大约 120dp 宽。
 */
@Composable
fun posterGridColumns(): GridCells {
    val widthDp = LocalConfiguration.current.screenWidthDp
    val columns = when {
        widthDp >= 1000 -> 8
        widthDp >= 840 -> 7
        widthDp >= 720 -> 6
        widthDp >= 600 -> 5
        widthDp >= 480 -> 4
        else -> 3
    }
    return GridCells.Fixed(columns)
}

/** 选集抽屉：小格子，平板/横屏多排几列 */
@Composable
fun episodeGridColumns(): GridCells {
    val widthDp = LocalConfiguration.current.screenWidthDp
    val columns = when {
        widthDp >= 900 -> 10
        widthDp >= 720 -> 8
        widthDp >= 520 -> 6
        else -> 4
    }
    return GridCells.Fixed(columns)
}
