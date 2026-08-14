package com.zedscope.app

import android.os.Handler
import android.os.Looper
import android.webkit.WebView
import org.json.JSONObject
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Drives the in-app WebView from AI browser commands — WITHOUT any Android
 * permission. The AI's instructions are executed inside the page's own context
 * via evaluateJavascript (equivalent to typing JS in the dev console), so no
 * system capability is touched. A background thread polls YamiCore for pending
 * commands and posts the actual JS evaluation to the main thread.
 */
class AiBridge(private val webView: WebView) {
    private val handler = Handler(Looper.getMainLooper())
    private var running = false

    fun start() {
        if (running) return
        running = true
        Thread { loop() }.start()
    }

    fun stop() { running = false }

    private fun loop() {
        while (running) {
            try {
                val pending = YamiCore.aibrowserPending()
                if (pending.isNotBlank()) {
                    execute(JSONObject(pending))
                }
            } catch (_: Throwable) {
            }
            Thread.sleep(400)
        }
    }

    private fun execute(cmd: JSONObject) {
        val id = cmd.optString("id")
        val action = cmd.optString("action")
        val res = when (action) {
            "navigate" -> {
                val url = cmd.optString("url")
                handler.post { webView.loadUrl(url) }
                Thread.sleep(1500) // allow navigation to settle
                "navigated to $url"
            }
            "click" -> jsEval(
                "(function(){var e=document.querySelector(${q(cmd.optString("selector"))});" +
                "if(e){e.click();return 'clicked';}return 'element not found';})()"
            )
            "type" -> jsEval(
                "(function(){var e=document.querySelector(${q(cmd.optString("selector"))});" +
                "if(e){e.value=${q(cmd.optString("text"))};" +
                "e.dispatchEvent(new Event('input',{bubbles:true}));return 'typed';}" +
                "return 'element not found';})()"
            )
            "extract" -> jsEval("document.body?document.body.innerText:''")
            else -> "unknown action: $action"
        }
        YamiCore.aibrowserComplete(id, res)
    }

    // Evaluate JS on the main thread and wait for the string result.
    private fun jsEval(js: String): String {
        val latch = CountDownLatch(1)
        val out = arrayOf("")
        handler.post {
            webView.evaluateJavascript(js) { v ->
                out[0] = v ?: ""
                latch.countDown()
            }
        }
        try { latch.await(8, TimeUnit.SECONDS) } catch (_: Throwable) {}
        return out[0]
            .replace("\\n", "\n")
            .replace("\\\"", "\"")
            .replace("\\u003C", "<")
            .trim('"')
    }

    // JSON-encode a string as a JS string literal (valid inside querySelector("...")).
    private fun q(s: String): String = JSONObject.quote(s)
}
