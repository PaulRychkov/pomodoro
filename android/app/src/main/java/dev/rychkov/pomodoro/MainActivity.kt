package dev.rychkov.pomodoro

import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.os.Bundle
import android.view.Menu
import android.view.MenuItem
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.Toast
import mobile.Mobile

class MainActivity : Activity() {

    private lateinit var web: WebView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startServer()
        web = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            webViewClient = WebViewClient()
            loadUrl(Mobile.baseURL())
        }
        setContentView(web)
    }

    override fun onDestroy() {
        super.onDestroy()
        if (isFinishing) {
            Mobile.stop()
        }
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
    }
}
