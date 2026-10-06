package dev.rychkov.pomodoro

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.PowerManager

class TimerAlarmReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val power = context.getSystemService(Context.POWER_SERVICE) as PowerManager
        power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "pomodoro:timer-end").acquire(AWAKE_AFTER_END_MS)
    }

    private companion object {
        const val AWAKE_AFTER_END_MS = 15_000L
    }
}
