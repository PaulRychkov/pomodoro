package dev.rychkov.pomodoro

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.media.RingtoneManager
import android.os.Build
import android.os.IBinder
import mobile.Mobile
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

class TimerService : Service() {

    @Volatile
    private var running = false
    private var worker: Thread? = null
    private var prevPhase: String? = null
    private var prevRemaining: Int = 0

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createChannels()
        startForeground(NOTIF_ID, buildNotification("Помидор", "Ожидание", 0, 0))
        startEngine()
        running = true
        worker = Thread {
            while (running) {
                try {
                    tick()
                } catch (_: Exception) {
                }
                try {
                    Thread.sleep(1000)
                } catch (_: InterruptedException) {
                    return@Thread
                }
            }
        }.also { it.isDaemon = true; it.start() }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startEngine()
        return START_STICKY
    }

    override fun onDestroy() {
        running = false
        worker?.interrupt()
        Mobile.stop()
        super.onDestroy()
    }

    private fun startEngine() {
        val prefs = getSharedPreferences("sync", Context.MODE_PRIVATE)
        Mobile.start(
            filesDir.absolutePath,
            prefs.getString("tasks_url", "") ?: "",
            prefs.getString("url", "") ?: "",
            prefs.getString("token", "") ?: "",
        )
    }

    private fun tick() {
        val body = fetchState() ?: return
        val json = JSONObject(body)
        val phase = json.optString("phase", "idle")
        val remaining = json.optInt("remaining_seconds", 0)
        val planned = json.optInt("planned_seconds", 0)
        val paused = json.optBoolean("paused", false)
        val done = json.optInt("completed_today", 0)
        val total = json.optInt("day_total", 0)

        val wasRunning = prevPhase != null && prevPhase != "idle"
        if (wasRunning && phase != prevPhase && prevRemaining <= 2) {
            notifyFinished(prevPhase!!)
        }
        prevPhase = phase
        prevRemaining = remaining

        val title = when {
            phase == "focus" && paused -> "Фокус на паузе"
            phase == "focus" -> "Фокус"
            phase == "short_break" -> "Короткий перерыв"
            phase == "long_break" -> "Длинный перерыв"
            else -> "Помидор"
        }
        val text = if (phase == "idle") {
            if (total > 0) "Сегодня: $done из $total" else "Таймер не запущен"
        } else {
            "${formatTime(remaining)} · сегодня $done из $total"
        }
        notificationManager().notify(
            NOTIF_ID,
            buildNotification(title, text, planned, planned - remaining),
        )
    }

    private fun fetchState(): String? {
        val conn = URL(STATE_URL).openConnection() as HttpURLConnection
        return try {
            conn.connectTimeout = 2000
            conn.readTimeout = 2000
            if (conn.responseCode != 200) null else conn.inputStream.bufferedReader().readText()
        } finally {
            conn.disconnect()
        }
    }

    private fun buildNotification(title: String, text: String, max: Int, progress: Int): Notification {
        val open = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE,
        )
        val b = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_TIMER)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        b.setContentTitle(title)
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_lock_idle_alarm)
            .setContentIntent(open)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
        if (max > 0) {
            b.setProgress(max, progress.coerceIn(0, max), false)
        }
        return b.build()
    }

    private fun notifyFinished(finishedPhase: String) {
        val focus = finishedPhase == "focus"
        val b = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ALERT)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        b.setContentTitle(if (focus) "Помидор завершён" else "Перерыв закончился")
            .setContentText(if (focus) "Пора отдохнуть" else "Можно продолжать")
            .setSmallIcon(android.R.drawable.ic_lock_idle_alarm)
            .setAutoCancel(true)
        notificationManager().notify(NOTIF_ALERT_ID, b.build())
        playAlertSound()
    }

    private fun playAlertSound() {
        try {
            val saved = getSharedPreferences("sync", Context.MODE_PRIVATE).getString("sound_uri", "")
            val uri = if (!saved.isNullOrEmpty()) {
                Uri.parse(saved)
            } else {
                RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION)
            }
            RingtoneManager.getRingtone(applicationContext, uri)?.play()
        } catch (_: Exception) {
        }
    }

    private fun createChannels() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val mgr = notificationManager()
        mgr.createNotificationChannel(
            NotificationChannel(CHANNEL_TIMER, "Таймер", NotificationManager.IMPORTANCE_LOW).apply {
                setShowBadge(false)
                enableVibration(false)
            },
        )
        val alert = NotificationChannel(CHANNEL_ALERT, "Сигнал таймера", NotificationManager.IMPORTANCE_HIGH)
        alert.setSound(null, null)
        alert.enableVibration(true)
        mgr.createNotificationChannel(alert)
    }

    private fun notificationManager() =
        getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    private fun formatTime(sec: Int): String {
        val s = if (sec < 0) 0 else sec
        return "%02d:%02d".format(s / 60, s % 60)
    }

    companion object {
        private const val CHANNEL_TIMER = "pomodoro_timer"
        private const val CHANNEL_ALERT = "pomodoro_alert"
        private const val NOTIF_ID = 1
        private const val NOTIF_ALERT_ID = 2
        private const val STATE_URL = "http://127.0.0.1:18082/api/v1/state"

        fun start(ctx: Context) {
            val i = Intent(ctx, TimerService::class.java)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                ctx.startForegroundService(i)
            } else {
                ctx.startService(i)
            }
        }

        fun stop(ctx: Context) {
            ctx.stopService(Intent(ctx, TimerService::class.java))
        }
    }
}
