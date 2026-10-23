package com.juku.app.di

import android.content.Context
import androidx.room.Room
import coil.ImageLoader
import com.juku.app.data.local.db.AppDatabase
import com.juku.app.data.local.db.DownloadTaskDao
import com.juku.app.data.local.db.FollowingDao
import com.juku.app.data.local.db.HistoryDao
import com.juku.app.data.local.prefs.AppPrefs
import com.juku.app.data.remote.BaseUrlInterceptor
import com.juku.app.data.remote.JukuApi
import com.juku.app.data.remote.JukuCookieJar
import com.juku.app.data.remote.SessionAuthenticator
import com.juku.app.data.remote.ViewerInterceptor
import com.juku.app.data.repository.AuthRepositoryImpl
import com.juku.app.data.repository.DramaRepositoryImpl
import com.juku.app.data.repository.DownloadRepositoryImpl
import com.juku.app.data.repository.FollowingRepositoryImpl
import com.juku.app.data.repository.HistoryRepositoryImpl
import com.juku.app.data.repository.PlaybackRepositoryImpl
import com.juku.app.domain.repository.AuthRepository
import com.juku.app.domain.repository.DramaRepository
import com.juku.app.domain.repository.DownloadRepository
import com.juku.app.domain.repository.FollowingRepository
import com.juku.app.domain.repository.HistoryRepository
import com.juku.app.domain.repository.PlaybackRepository
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import okhttp3.OkHttpClient
import retrofit2.Retrofit
import retrofit2.converter.gson.GsonConverterFactory
import java.util.concurrent.TimeUnit
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object DatabaseModule {

    @Provides
    @Singleton
    fun provideDatabase(@ApplicationContext context: Context): AppDatabase =
        Room.databaseBuilder(context, AppDatabase::class.java, "juku.db")
            .fallbackToDestructiveMigration()
            .build()

    @Provides
    fun provideHistoryDao(db: AppDatabase): HistoryDao = db.historyDao()

    @Provides
    fun provideFollowingDao(db: AppDatabase): FollowingDao = db.followingDao()

    @Provides
    fun provideDownloadTaskDao(db: AppDatabase): DownloadTaskDao = db.downloadTaskDao()
}

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {

    @Provides
    @Singleton
    fun provideCookieJar(prefs: AppPrefs): JukuCookieJar = JukuCookieJar(prefs)

    /** 所有 OkHttp 客户端共享的基础配置（Cookie/身份头/直连/401 自愈），再按用途加超时与日志 */
    private fun baseClientBuilder(prefs: AppPrefs, cookieJar: JukuCookieJar): OkHttpClient.Builder =
        OkHttpClient.Builder()
            .cookieJar(cookieJar)
            .addInterceptor(BaseUrlInterceptor { prefs.serverUrl })
            .addInterceptor(ViewerInterceptor { prefs.viewerId })
            .authenticator(SessionAuthenticator(cookieJar, prefs))
            .proxy(java.net.Proxy.NO_PROXY)  // 绕过系统代理

    @Provides
    @Singleton
    fun provideOkHttpClient(prefs: AppPrefs, cookieJar: JukuCookieJar): OkHttpClient =
        baseClientBuilder(prefs, cookieJar)
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .writeTimeout(60, TimeUnit.SECONDS)
            .apply {
                if (com.juku.app.BuildConfig.DEBUG) {
                    addInterceptor(
                        okhttp3.logging.HttpLoggingInterceptor().apply {
                            level = okhttp3.logging.HttpLoggingInterceptor.Level.BODY
                        }
                    )
                }
            }
            .build()

    @Provides
    @Singleton
    fun provideRetrofit(client: OkHttpClient): Retrofit =
        Retrofit.Builder()
            .baseUrl("http://${BaseUrlInterceptor.PLACEHOLDER_HOST}/")
            .client(client)
            .addConverterFactory(
                GsonConverterFactory.create(
                    com.google.gson.GsonBuilder().disableHtmlEscaping().create()
                )
            )
            .build()

    @Provides
    @Singleton
    fun provideJukuApi(retrofit: Retrofit): JukuApi = retrofit.create(JukuApi::class.java)

    /**
     * 图片加载独立客户端：不带日志拦截器（BODY 级日志会把每张封面响应体
     * 序列化成字符串，滚动剧库时是明显的卡顿源），网络栈与主客户端一致。
     */
    @Provides
    @Singleton
    fun provideImageLoader(
        @ApplicationContext context: Context,
        prefs: AppPrefs,
        cookieJar: JukuCookieJar
    ): ImageLoader =
        ImageLoader.Builder(context)
            .okHttpClient(
                baseClientBuilder(prefs, cookieJar)
                    .connectTimeout(15, TimeUnit.SECONDS)
                    .readTimeout(30, TimeUnit.SECONDS)
                    .build()
            )
            .crossfade(true)
            // 海报是不透明图，RGB_565 每像素 2 字节省一半位图内存，减少 GC 卡顿
            .allowRgb565(true)
            .build()

    /**
     * 独立的播放专用 OkHttp 客户端：与主客户端共享 CookieJar 和 Viewer 拦截器，
     * 但不包含 BaseUrlInterceptor（ExoPlayer 会直接请求真实地址，不需要 host 重写）。
     */
    @PlaybackOkHttp
    @Provides
    @Singleton
    fun providePlaybackOkHttpClient(prefs: AppPrefs, cookieJar: JukuCookieJar): OkHttpClient {
        val builder = OkHttpClient.Builder()
            .cookieJar(cookieJar)
            .addInterceptor(ViewerInterceptor { prefs.viewerId })
            .authenticator(SessionAuthenticator(cookieJar, prefs))
            .proxy(java.net.Proxy.NO_PROXY)  // 绕过系统代理，直连服务器
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(120, TimeUnit.SECONDS)
        if (com.juku.app.BuildConfig.DEBUG) {
            builder.addInterceptor(
                okhttp3.logging.HttpLoggingInterceptor().apply {
                    level = okhttp3.logging.HttpLoggingInterceptor.Level.BASIC
                }
            )
        }
        return builder.build()
    }
}

@Module
@InstallIn(SingletonComponent::class)
abstract class RepositoryModule {

    @Binds
    abstract fun bindDramaRepository(impl: DramaRepositoryImpl): DramaRepository

    @Binds
    abstract fun bindAuthRepository(impl: AuthRepositoryImpl): AuthRepository

    @Binds
    abstract fun bindPlaybackRepository(impl: PlaybackRepositoryImpl): PlaybackRepository

    @Binds
    abstract fun bindFollowingRepository(impl: FollowingRepositoryImpl): FollowingRepository

    @Binds
    abstract fun bindHistoryRepository(impl: HistoryRepositoryImpl): HistoryRepository

    @Binds
    abstract fun bindDownloadRepository(impl: DownloadRepositoryImpl): DownloadRepository
}
