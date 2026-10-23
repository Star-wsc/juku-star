package com.juku.app

import android.app.Application
import androidx.hilt.work.HiltWorkerFactory
import androidx.work.Configuration
import coil.ImageLoader
import com.juku.app.domain.repository.DownloadRepository
import com.juku.app.service.DownloadWorker
import dagger.hilt.android.HiltAndroidApp
import javax.inject.Inject

@HiltAndroidApp
class JukuApp : Application(), Configuration.Provider, coil.ImageLoaderFactory {

    @Inject lateinit var workerFactory: HiltWorkerFactory
    @Inject lateinit var imageLoader: ImageLoader
    @Inject lateinit var downloadRepository: DownloadRepository

    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder()
            .setWorkerFactory(workerFactory)
            .build()

    override fun onCreate() {
        super.onCreate()
        downloadRepository.ensureWorkerEnqueued()
    }

    override fun newImageLoader(): ImageLoader = imageLoader
}
