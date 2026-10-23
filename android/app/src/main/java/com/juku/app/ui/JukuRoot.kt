package com.juku.app.ui

import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import com.juku.app.MainViewModel
import com.juku.app.ui.screens.auth.AuthScreen
import com.juku.app.ui.screens.detail.DetailScreen
import com.juku.app.ui.screens.downloads.DownloadsScreen
import com.juku.app.ui.screens.following.FollowingScreen
import com.juku.app.ui.screens.history.HistoryScreen
import com.juku.app.ui.screens.home.HomeScreen
import com.juku.app.ui.screens.player.PlayerScreen
import com.juku.app.ui.screens.search.SearchScreen
import com.juku.app.ui.screens.settings.SettingsScreen

object Routes {
    const val HOME = "home"
    const val FOLLOWING = "following"
    const val DOWNLOADS = "downloads"
    const val HISTORY = "history"
    const val SETTINGS = "settings"
    const val SEARCH = "search"
    const val DETAIL = "detail/{dramaId}"
    const val PLAYER = "player/{dramaId}?episode={episode}&offline={offline}&fromHistory={fromHistory}"

    fun detail(dramaId: String) = "detail/${android.net.Uri.encode(dramaId)}"

    fun player(dramaId: String, episode: Int? = null, offline: Boolean = false, fromHistory: Boolean = false): String {
        val id = android.net.Uri.encode(dramaId)
        return "player/$id?episode=${episode ?: -1}&offline=$offline&fromHistory=$fromHistory"
    }
}

private data class Tab(
    val route: String,
    val label: String,
    val selectedIcon: ImageVector,
    val unselectedIcon: ImageVector
)

private val tabs = listOf(
    Tab(Routes.HOME, "剧库", Icons.Filled.Home, Icons.Outlined.Home),
    Tab(Routes.FOLLOWING, "追剧", Icons.Filled.Favorite, Icons.Outlined.FavoriteBorder),
    Tab(Routes.DOWNLOADS, "下载", Icons.Filled.Download, Icons.Outlined.Download),
    Tab(Routes.HISTORY, "历史", Icons.Filled.History, Icons.Outlined.History),
    Tab(Routes.SETTINGS, "设置", Icons.Filled.Settings, Icons.Outlined.Settings)
)

@Composable
fun JukuRoot(viewModel: MainViewModel) {
    val state by viewModel.state.collectAsStateWithLifecycle()

    // 未登录/未连接时显示整页认证页（服务器 + 账密一屏搞定）
    if (state.showAuth) {
        AuthScreen(onEntered = { viewModel.onAuthEntered() })
        return
    }

    // 首次进入首页弹出免责声明（只一次，10 秒后才能关闭）
    val disclaimerContext = androidx.compose.ui.platform.LocalContext.current
    var showDisclaimer by androidx.compose.runtime.remember {
        androidx.compose.runtime.mutableStateOf(
            !disclaimerContext.getSharedPreferences("juku_prefs", android.content.Context.MODE_PRIVATE)
                .getBoolean("disclaimer_accepted", false)
        )
    }
    var disclaimerSecondsLeft by androidx.compose.runtime.remember { androidx.compose.runtime.mutableStateOf(10) }
    if (showDisclaimer) {
        androidx.compose.runtime.LaunchedEffect(Unit) {
            while (disclaimerSecondsLeft > 0) {
                kotlinx.coroutines.delay(1000)
                disclaimerSecondsLeft -= 1
            }
        }
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { /* 须点按钮关闭 */ },
            title = { Text("使用声明") },
            text = {
                Text(
                    "欢迎使用「短剧库APP」。\n\n" +
                        "1. 本软件仅提供本地播放器与下载管理能力，不生产、不编辑、不上传任何影视内容。\n" +
                        "2. 片源、剧集、封面、简介等全部内容均来自您自行配置的第三方服务器及其上游项目，与本软件无关。\n" +
                        "3. 本软件不存储、不转发上述内容；播放与下载仅在您的设备与所配置服务器之间进行。\n" +
                        "4. 请确保合法使用相关内容并遵守当地法律法规；因服务器内容产生的争议与责任由内容提供方及使用者承担。\n" +
                        "5. 登录注册均针对您配置的服务器，本软件不提供账号、计费或客服。\n\n" +
                        "继续使用即表示您已阅读并理解上述声明。"
                )
            },
            confirmButton = {
                TextButton(
                    enabled = disclaimerSecondsLeft <= 0,
                    onClick = {
                        disclaimerContext.getSharedPreferences("juku_prefs", android.content.Context.MODE_PRIVATE)
                            .edit().putBoolean("disclaimer_accepted", true).apply()
                        showDisclaimer = false
                    }
                ) {
                    Text(if (disclaimerSecondsLeft > 0) "我已知晓（${disclaimerSecondsLeft}s）" else "我已阅读并知晓")
                }
            }
        )
    }

    val navController = rememberNavController()
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route
    val showBottomBar = tabs.any { it.route == currentRoute }

    Scaffold(
        // 外层 Scaffold 不注入内容 insets：状态栏由各页面 TopAppBar 自行避让，
        // 底部导航栏 NavigationBar 已自带 gesture insets 避让
        contentWindowInsets = WindowInsets(0, 0, 0, 0),
        bottomBar = {
            if (showBottomBar) {
                NavigationBar {
                    tabs.forEach { tab ->
                        val selected = currentRoute == tab.route
                        NavigationBarItem(
                            selected = selected,
                            onClick = { navController.navigateTab(tab.route) },
                            icon = {
                                Icon(
                                    if (selected) tab.selectedIcon else tab.unselectedIcon,
                                    contentDescription = tab.label
                                )
                            },
                            label = { Text(tab.label) }
                        )
                    }
                }
            }
        }
    ) { innerPadding ->
        NavHost(
            navController = navController,
            startDestination = Routes.HOME,
            modifier = Modifier.padding(innerPadding)
        ) {
            composable(Routes.HOME) {
                HomeScreen(
                    onOpenDrama = { navController.navigateDetail(it) },
                    onOpenSearch = { navController.navigate(Routes.SEARCH) }
                )
            }
            composable(Routes.FOLLOWING) {
                FollowingScreen(onOpenDrama = { navController.navigateDetail(it) })
            }
            composable(Routes.DOWNLOADS) {
                DownloadsScreen(onOpenDrama = { navController.navigateDetail(it) })
            }
            composable(Routes.HISTORY) {
                HistoryScreen(
                    onOpenDrama = { navController.navigateDetail(it) },
                    onPlay = { dramaId ->
                        navController.navigate(Routes.player(dramaId, fromHistory = true))
                    }
                )
            }
            composable(Routes.SETTINGS) {
                SettingsScreen(onShowAuth = { viewModel.onLoggedOut() })
            }
            composable(Routes.SEARCH) {
                SearchScreen(
                    onBack = { navController.popBackStack() },
                    onOpenDrama = { navController.navigateDetail(it) }
                )
            }
            composable(
                Routes.DETAIL,
                arguments = listOf(navArgument("dramaId") { type = NavType.StringType })
            ) {
                DetailScreen(
                    onBack = { navController.popBackStack() },
                    onPlay = { dramaId, episode, offline ->
                        navController.navigate(Routes.player(dramaId, episode, offline))
                    }
                )
            }
            composable(
                Routes.PLAYER,
                arguments = listOf(
                    navArgument("dramaId") { type = NavType.StringType },
                    navArgument("episode") { type = NavType.IntType; defaultValue = -1 },
                    navArgument("offline") { type = NavType.BoolType; defaultValue = false },
                    navArgument("fromHistory") { type = NavType.BoolType; defaultValue = false }
                ),
                // 退出用 slide+fade，纯 fadeOut 对 SurfaceView 无效会卡住
                enterTransition = { fadeIn() + slideInVertically(initialOffsetY = { it / 3 }) },
                exitTransition = { fadeOut() },
                popEnterTransition = { fadeIn() },
                popExitTransition = { slideOutVertically(targetOffsetY = { it }) + fadeOut() }
            ) {
                PlayerScreen(onBack = { navController.popBackStack() })
            }
        }
    }
}

private fun NavHostController.navigateTab(route: String) {
    navigate(route) {
        popUpTo(graph.findStartDestination().id) { saveState = true }
        launchSingleTop = true
        restoreState = true
    }
}

private fun NavHostController.navigateDetail(dramaId: String) {
    navigate(Routes.detail(dramaId))
}
