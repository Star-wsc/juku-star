package com.juku.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val LightColors = lightColorScheme(
    primary = Color(0xFF1A73E8),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFD6E3FF),
    onPrimaryContainer = Color(0xFF001B3F),
    secondary = Color(0xFF565E71),
    onSecondary = Color.White,
    // FilterChip 选中态、导航栏选中指示用的就是 secondaryContainer，
    // 不覆盖的话会回落到 Material 默认的淡紫色
    secondaryContainer = Color(0xFFD6E3FF),
    onSecondaryContainer = Color(0xFF001B3F),
    surface = Color(0xFFFCF8F8),
    onSurface = Color(0xFF1B1B1F),
    surfaceVariant = Color(0xFFE1E2EC),
    onSurfaceVariant = Color(0xFF44474F),
    background = Color(0xFFFCF8F8),
    // 底部导航/菜单/输入框容器色系（默认值同为淡紫，需压成蓝灰）
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFF6F8FB),
    surfaceContainer = Color(0xFFF0F3F8),
    surfaceContainerHigh = Color(0xFFEAEEF4),
    surfaceContainerHighest = Color(0xFFE4E8EF),
    outline = Color(0xFF74798A),
    outlineVariant = Color(0xFFC4C9D6)
)

private val DarkColors = darkColorScheme(
    primary = Color(0xFFA8C8FF),
    onPrimary = Color(0xFF00306F),
    primaryContainer = Color(0xFF00478E),
    onPrimaryContainer = Color(0xFFD6E3FF),
    secondary = Color(0xFFBEC6DC),
    onSecondary = Color(0xFF253140),
    secondaryContainer = Color(0xFF3B4C66),
    onSecondaryContainer = Color(0xFFD6E3FF),
    surface = Color(0xFF1B1B1F),
    onSurface = Color(0xFFE3E2E6),
    surfaceVariant = Color(0xFF44474F),
    onSurfaceVariant = Color(0xFFC4C6CD),
    background = Color(0xFF121316),
    surfaceContainerLowest = Color(0xFF0E0F12),
    surfaceContainerLow = Color(0xFF1A1C20),
    surfaceContainer = Color(0xFF1E2024),
    surfaceContainerHigh = Color(0xFF292A2F),
    surfaceContainerHighest = Color(0xFF34353A),
    outline = Color(0xFF8E9199),
    outlineVariant = Color(0xFF474B54)
)

/**
 * 应用主题。注意：不使用 Material You 动态取色（dynamicColorScheme）——
 * Android 12+ 上壁纸取色会把下面的品牌配色整体覆盖掉，导致自定义主题"不生效"。
 * 浅色/深色由 themeMode 决定（light | dark | system）。
 */
@Composable
fun JukuTheme(themeMode: String = "system", content: @Composable () -> Unit) {
    val darkTheme = when (themeMode) {
        "dark" -> true
        "light" -> false
        else -> isSystemInDarkTheme()
    }
    MaterialTheme(
        colorScheme = if (darkTheme) DarkColors else LightColors,
        content = content
    )
}
