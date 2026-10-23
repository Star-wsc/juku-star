package com.juku.app

import android.app.PictureInPictureParams
import android.os.Build
import android.os.Bundle
import android.util.Rational
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.runtime.getValue
import com.juku.app.ui.JukuRoot
import com.juku.app.ui.theme.JukuTheme
import com.juku.app.util.PipGate
import dagger.hilt.android.AndroidEntryPoint

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent {
            val vm: MainViewModel = viewModel()
            // 订阅主题流：设置里切换浅色/深色/跟随系统立即生效，无需重启
            val themeMode by vm.themeMode.collectAsStateWithLifecycle()
            JukuTheme(themeMode = themeMode) {
                JukuRoot(viewModel = vm)
            }
        }
    }

    /** 播放中按 Home / 切后台 → 自动进小窗 */
    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        if (PipGate.shouldAutoEnter) {
            enterPictureInPictureCompat()
        }
    }

    /** 手动小窗（播放器按钮） */
    fun enterPictureInPictureCompat(): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return false
        if (isInPictureInPictureMode) return true
        return try {
            val params = PictureInPictureParams.Builder()
                .setAspectRatio(Rational(16, 9))
                .build()
            enterPictureInPictureMode(params)
        } catch (_: Exception) {
            false
        }
    }
}
