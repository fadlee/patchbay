package com.fadlee.patchbay.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import android.util.Log
import androidx.core.app.NotificationCompat
import com.fadlee.patchbay.MainActivity
import com.fadlee.patchbay.R
import patchbay.Patchbay
import java.io.File

class PatchbayService : Service() {

    companion object {
        private const val TAG = "PatchbayService"
        const val ACTION_START = "com.fadlee.patchbay.ACTION_START"
        const val ACTION_STOP = "com.fadlee.patchbay.ACTION_STOP"
        const val CHANNEL_ID = "patchbay_service_channel"
        const val NOTIFICATION_ID = 8787

        const val DEFAULT_ADMIN_HOST = "127.0.0.1"
        const val DEFAULT_ADMIN_PORT = 8787

        @Volatile
        var isServiceRunning = false
            private set

        fun startService(context: Context) {
            val intent = Intent(context, PatchbayService::class.java).apply {
                action = ACTION_START
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stopService(context: Context) {
            val intent = Intent(context, PatchbayService::class.java).apply {
                action = ACTION_STOP
            }
            context.startService(intent)
        }
    }

    private var wakeLock: PowerManager.WakeLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
        acquireWakeLock()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopPatchbay()
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf()
                return START_NOT_STICKY
            }
            ACTION_START, null -> {
                startPatchbay()
            }
        }
        return START_STICKY
    }

    private fun startPatchbay() {
        if (isServiceRunning && Patchbay.isRunning()) {
            return
        }

        val notification = createNotification("127.0.0.1:8787")
        startForeground(NOTIFICATION_ID, notification)

        Thread {
            try {
                val dataDir = filesDir.absolutePath
                Log.d(TAG, "Starting Patchbay in dataDir: $dataDir")
                Patchbay.start(dataDir, DEFAULT_ADMIN_HOST, DEFAULT_ADMIN_PORT.toLong())
                isServiceRunning = true
                Log.i(TAG, "Patchbay started successfully on port $DEFAULT_ADMIN_PORT")
            } catch (e: Exception) {
                Log.e(TAG, "Failed to start Patchbay Go engine", e)
                stopSelf()
            }
        }.start()
    }

    private fun stopPatchbay() {
        Thread {
            try {
                Patchbay.stop()
                isServiceRunning = false
                Log.i(TAG, "Patchbay stopped successfully")
            } catch (e: Exception) {
                Log.e(TAG, "Error stopping Patchbay", e)
            }
        }.start()
    }

    private fun createNotification(hostPort: String): Notification {
        val openIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        val openPendingIntent = PendingIntent.getActivity(
            this, 0, openIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        val stopIntent = Intent(this, PatchbayService::class.java).apply {
            action = ACTION_STOP
        }
        val stopPendingIntent = PendingIntent.getService(
            this, 1, stopIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(getString(R.string.service_running_title))
            .setContentText(getString(R.string.service_running_desc, hostPort))
            .setContentIntent(openPendingIntent)
            .setOngoing(true)
            .addAction(0, getString(R.string.action_stop), stopPendingIntent)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = getString(R.string.notification_channel_desc)
                setShowBadge(false)
            }
            val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            manager.createNotificationChannel(channel)
        }
    }

    private fun acquireWakeLock() {
        val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
        wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "Patchbay::ProxyWakeLock").apply {
            setReferenceCounted(false)
            acquire(10 * 60 * 1000L /* 10 minutes fallback, refreshed while running */)
        }
    }

    override fun onDestroy() {
        stopPatchbay()
        try {
            if (wakeLock?.isHeld == true) {
                wakeLock?.release()
            }
        } catch (e: Exception) {
            Log.w(TAG, "Error releasing wakelock", e)
        }
        isServiceRunning = false
        super.onDestroy()
    }
}
