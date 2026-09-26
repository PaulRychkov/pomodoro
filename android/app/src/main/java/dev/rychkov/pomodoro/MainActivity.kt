package dev.rychkov.pomodoro

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.media.RingtoneManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.view.Menu
import android.view.MenuItem
import android.webkit.JavascriptInterface
import android.webkit.WebChromeClient
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.Toast
import mobile.Mobile

class MainActivity : Activity() {

    private lateinit var web: WebView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestNotificationsIfNeeded()
        TimerService.start(this)
        startServer()
        actionBar?.hide()
        web = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.textZoom = 100
            webViewClient = WebViewClient()
            webChromeClient = WebChromeClient()
            addJavascriptInterface(JsHost(), "AndroidHost")
            loadUrl(Mobile.baseURL())
        }
        val root = FrameLayout(this).apply { fitsSystemWindows = true }
        root.addView(
            web,
            FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.MATCH_PARENT,
            ),
        )
        setContentView(root)
    }

    inner class JsHost {
        @JavascriptInterface
        fun openSync() {
            runOnUiThread { showSettingsDialog() }
        }

        @JavascriptInterface
        fun pickSound() {
            runOnUiThread {
                val current = prefs().getString("sound_uri", "") ?: ""
                val intent = Intent(RingtoneManager.ACTION_RINGTONE_PICKER).apply {
                    putExtra(RingtoneManager.EXTRA_RINGTONE_TYPE, RingtoneManager.TYPE_NOTIFICATION)
                    putExtra(RingtoneManager.EXTRA_RINGTONE_TITLE, "Сигнал окончания помидора")
                    putExtra(RingtoneManager.EXTRA_RINGTONE_SHOW_DEFAULT, true)
                    putExtra(RingtoneManager.EXTRA_RINGTONE_SHOW_SILENT, false)
                    if (current.isNotEmpty()) {
                        putExtra(RingtoneManager.EXTRA_RINGTONE_EXISTING_URI, Uri.parse(current))
                    }
                }
                startActivityForResult(intent, REQ_PICK_SOUND)
            }
        }
    }

    @Deprecated("Activity без AndroidX — иного колбэка выбора мелодии здесь нет")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        @Suppress("DEPRECATION")
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != REQ_PICK_SOUND || resultCode != RESULT_OK) return
        val uri: Uri? = data?.getParcelableExtra(RingtoneManager.EXTRA_RINGTONE_PICKED_URI)
        prefs().edit().putString("sound_uri", uri?.toString() ?: "").apply()
        val title = if (uri == null) {
            "звук отключён"
        } else {
            RingtoneManager.getRingtone(this, uri)?.getTitle(this) ?: "выбранная мелодия"
        }
        Toast.makeText(this, "Сигнал таймера: $title", Toast.LENGTH_SHORT).show()
        web.evaluateJavascript(
            "window.dispatchEvent(new CustomEvent('android-sound-picked',{detail:${jsString(title)}}))",
            null,
        )
    }

    private fun jsString(s: String): String = "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"") + "\""

    private fun requestNotificationsIfNeeded() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED) return
        requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQ_NOTIFICATIONS)
    }

    override fun onCreateOptionsMenu(menu: Menu): Boolean {
        menu.add(0, MENU_SETTINGS, 0, getString(R.string.menu_sync))
        return true
    }

    override fun onOptionsItemSelected(item: MenuItem): Boolean {
        if (item.itemId == MENU_SETTINGS) {
            showSettingsDialog()
            return true
        }
        return super.onOptionsItemSelected(item)
    }

    private fun prefs() = getSharedPreferences("sync", Context.MODE_PRIVATE)

    private fun startServer() {
        val err = Mobile.start(
            filesDir.absolutePath,
            prefs().getString("tasks_url", "") ?: "",
            prefs().getString("url", "") ?: "",
            prefs().getString("token", "") ?: "",
        )
        if (err.isNotEmpty()) {
            Toast.makeText(this, err, Toast.LENGTH_LONG).show()
        }
    }

    private fun showSettingsDialog() {
        val tasksInput = EditText(this).apply {
            hint = getString(R.string.tasks_url_hint)
            setText(prefs().getString("tasks_url", ""))
        }
        val urlInput = EditText(this).apply {
            hint = getString(R.string.sync_url_hint)
            setText(prefs().getString("url", ""))
        }
        val tokenInput = EditText(this).apply {
            hint = getString(R.string.sync_token_hint)
            setText(prefs().getString("token", ""))
        }
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 24, 48, 0)
            addView(tasksInput)
            addView(urlInput)
            addView(tokenInput)
        }
        AlertDialog.Builder(this)
            .setTitle(getString(R.string.menu_sync))
            .setView(box)
            .setPositiveButton(getString(R.string.sync_save)) { _, _ ->
                prefs().edit()
                    .putString("tasks_url", tasksInput.text.toString().trim().trimEnd('/'))
                    .putString("url", urlInput.text.toString().trim().trimEnd('/'))
                    .putString("token", tokenInput.text.toString().trim())
                    .apply()
                Mobile.stop()
                startServer()
                web.reload()
            }
            .setNegativeButton(getString(R.string.sync_cancel), null)
            .show()
    }

    private companion object {
        const val MENU_SETTINGS = 1
        const val REQ_NOTIFICATIONS = 100
        const val REQ_PICK_SOUND = 101
    }
}
