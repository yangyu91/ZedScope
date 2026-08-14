package com.yamiua.app

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.view.LayoutInflater
import android.view.View
import android.view.inputmethod.EditorInfo
import android.webkit.CookieManager
import android.webkit.WebChromeClient
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.ProgressBar
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout
import com.google.android.material.bottomnavigation.BottomNavigationView
import com.google.android.material.materialswitch.MaterialSwitch
import androidx.core.content.FileProvider
import androidx.webkit.ProxyConfig
import java.io.File

class MainActivity : AppCompatActivity() {

    private lateinit var webView: WebView
    private lateinit var etUrl: EditText
    private lateinit var bottomNav: BottomNavigationView
    private lateinit var flBrowser: android.widget.FrameLayout
    private lateinit var flCapture: android.widget.FrameLayout
    private lateinit var flToken: android.widget.FrameLayout
    private lateinit var flSettings: android.widget.FrameLayout
    private lateinit var flAi: android.widget.FrameLayout
    private lateinit var aiBridge: AiBridge
    private lateinit var swipeCapture: SwipeRefreshLayout
    private lateinit var rvCapture: RecyclerView
    private lateinit var rvToken: RecyclerView
    private lateinit var btnCopyAll: com.google.android.material.floatingactionbutton.FloatingActionButton
    private lateinit var btnCopyTokens: com.google.android.material.floatingactionbutton.FloatingActionButton
    private lateinit var progress: ProgressBar
    private lateinit var statRequests: TextView
    private lateinit var statTokens: TextView
    private lateinit var statLogins: TextView
    private lateinit var emptyCapture: TextView
    private lateinit var emptyToken: TextView
    private lateinit var switchClean: MaterialSwitch
    private lateinit var switchVpn: MaterialSwitch
    private lateinit var tvVpnStatus: TextView

    private val captureAdapter = CaptureAdapter()
    private val tokenAdapter = TokenAdapter()
    private var currentPanel = 0
    private var currentSessionId = ""   // 当前 AI 会话（省token）
    private lateinit var captureBanner: TextView   // 浏览器内"抓包为空"引导

    // v2 DEMO: VPN permission launcher for global (all-app) capture.
    private val vpnPermission = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) startVpn() else {
            switchVpn.isChecked = false
            Toast.makeText(this, R.string.vpn_permission_required, Toast.LENGTH_LONG).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        setTheme(R.style.Theme_Yami)   // swap splash theme back to the app theme
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        webView = findViewById(R.id.webview)
        etUrl = findViewById(R.id.etUrl)
        bottomNav = findViewById(R.id.bottomNav)
        flBrowser = findViewById(R.id.flBrowser)
        flCapture = findViewById(R.id.flCapture)
        flToken = findViewById(R.id.flToken)
        flSettings = findViewById(R.id.flSettings)
        flAi = findViewById(R.id.flAi)
        swipeCapture = findViewById(R.id.swipeCapture)
        rvCapture = findViewById(R.id.rvCapture)
        rvToken = findViewById(R.id.rvToken)
        btnCopyAll = findViewById(R.id.btnCopyAll)
        btnCopyTokens = findViewById(R.id.btnCopyTokens)
        progress = findViewById(R.id.progress)
        statRequests = findViewById(R.id.statRequests)
        statTokens = findViewById(R.id.statTokens)
        statLogins = findViewById(R.id.statLogins)
        emptyCapture = findViewById(R.id.emptyCapture)
        emptyToken = findViewById(R.id.emptyToken)
        switchClean = findViewById(R.id.switchClean)
        switchVpn = findViewById(R.id.switchVpn)
        tvVpnStatus = findViewById(R.id.tvVpnStatus)
        captureBanner = findViewById(R.id.captureBanner)

        setupWebView()
        setupPanels()
        setupSettings()

        if (YamiCore.start()) {
            YamiCore.setBodyDir(cacheDir.absolutePath + "/yami-bodies")
            YamiCore.setCleanCapture(true) // clean capture ON by default
            applyWebViewProxy()
            Toast.makeText(this, R.string.toast_proxy_started, Toast.LENGTH_SHORT).show()
        }

        if (YamiCore.aiStart()) {
            aiBridge = AiBridge(webView)
            aiBridge.start()
        }

        setupAiPanel()
        loadHome()
    }

    private fun setupWebView() {
        webView.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            loadWithOverviewMode = true
            useWideViewPort = true
            mixedContentMode = WebSettings.MIXED_CONTENT_ALWAYS_ALLOW
            userAgentString = webView.settings.userAgentString.replace("; wv", "")
            setSupportZoom(true)
            builtInZoomControls = true
            displayZoomControls = false
        }
        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView?, url: String?): Boolean {
                val u = url ?: return true
                // App-internal actions from the home page.
                if (u.startsWith("yami://install-ca")) { installCa(); return true }
                if (u.startsWith("yami://capture")) { showPanel(1); return true }
                if (u.startsWith("yami://settings")) { showPanel(3); return true }
                view?.loadUrl(u)
                return true
            }
            override fun onPageStarted(view: WebView?, url: String?, favicon: android.graphics.Bitmap?) {
                if (url != null && url.startsWith("http")) etUrl.setText(url)
                updateBanner()
            }
        }
        webView.webChromeClient = object : WebChromeClient() {
            override fun onProgressChanged(view: WebView?, newProgress: Int) {
                progress.visibility = if (newProgress >= 100) View.GONE else View.VISIBLE
                progress.progress = newProgress
            }
        }
    }

    private fun applyWebViewProxy() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            try {
                val config = ProxyConfig.Builder()
                    .addProxyRule(YamiCore.proxyAddr())
                    .build()
                val executor = java.util.concurrent.Executors.newSingleThreadExecutor()
                android.webkit.ProxyController.getInstance()
                    .setProxyOverride(config, executor, java.lang.Runnable { })
            } catch (e: Throwable) {
                // Proxy override is best-effort; never let it crash startup.
                e.printStackTrace()
            }
        } else {
            Toast.makeText(this, "代理拦截需要 Android 10+", Toast.LENGTH_LONG).show()
        }
    }

    private fun setupPanels() {
        rvCapture.layoutManager = LinearLayoutManager(this)
        rvCapture.adapter = captureAdapter
        rvToken.layoutManager = LinearLayoutManager(this)
        rvToken.adapter = tokenAdapter

        swipeCapture.setOnRefreshListener {
            refreshCapture()
            swipeCapture.isRefreshing = false
        }

        btnCopyAll.setOnClickListener { copyAllTokens() }
        btnCopyTokens.setOnClickListener { copyAllTokens() }

        findViewById<Button>(R.id.btnInstallCa).setOnClickListener { installCa() }
        findViewById<Button>(R.id.btnClear).setOnClickListener {
            YamiCore.clear()
            refreshCapture()
            updateBanner()
            Toast.makeText(this, "已清空", Toast.LENGTH_SHORT).show()
        }
        // 长按"清空"导出 HAR（抓包增强：ProxyPin 差距补全）
        findViewById<Button>(R.id.btnClear).setOnLongClickListener {
            exportHAR()
            true
        }

        captureBanner.setOnClickListener { installCa() }

        findViewById<View>(R.id.btnBack).setOnClickListener { if (webView.canGoBack()) webView.goBack() }
        findViewById<View>(R.id.btnForward).setOnClickListener { if (webView.canGoForward()) webView.goForward() }
        val btnGo = findViewById<View>(R.id.btnGo)
        btnGo.setOnClickListener { go() }

        etUrl.setOnEditorActionListener { _, actionId, _ ->
            if (actionId == EditorInfo.IME_ACTION_GO) { go(); true } else false
        }

        bottomNav.setOnItemSelectedListener { item ->
            val which = when (item.itemId) {
                R.id.nav_browser -> 0
                R.id.nav_capture -> 1
                R.id.nav_token -> 2
                R.id.nav_settings -> 3
                R.id.nav_ai -> 4
                else -> -1
            }
            if (which >= 0) showPanel(which)
            true
        }
        bottomNav.selectedItemId = R.id.nav_browser
    }

    private fun setupSettings() {
        // Clean capture (default ON)
        switchClean.isChecked = true
        switchClean.setOnCheckedChangeListener { _, isChecked ->
            YamiCore.setCleanCapture(isChecked)
        }

        // Global capture (DEMO) — default OFF
        switchVpn.setOnCheckedChangeListener { _, isChecked ->
            if (isChecked) {
                val intent = VpnService.prepare(this)
                if (intent != null) vpnPermission.launch(intent) else startVpn()
            } else {
                stopVpn()
            }
        }
    }

    private fun startVpn() {
        startService(Intent(this, VpnCaptureService::class.java).putExtra("proxy", YamiCore.proxyAddr()))
        tvVpnStatus.text = getString(R.string.vpn_status_on)
        Toast.makeText(this, R.string.toast_vpn_started, Toast.LENGTH_SHORT).show()
    }

    private fun stopVpn() {
        stopService(Intent(this, VpnCaptureService::class.java))
        tvVpnStatus.text = getString(R.string.vpn_status_off)
        Toast.makeText(this, R.string.toast_vpn_stopped, Toast.LENGTH_SHORT).show()
    }

    private fun setupAiPanel() {
        val name = findViewById<EditText>(R.id.etProviderName)
        val base = findViewById<EditText>(R.id.etProviderBase)
        val key = findViewById<EditText>(R.id.etProviderKey)
        val model = findViewById<EditText>(R.id.etProviderModel)
        val out = findViewById<TextView>(R.id.tvAiOutput)
        val etInput = findViewById<EditText>(R.id.etAiInput)
        val aiScroll = findViewById<View>(R.id.aiOutputScroll)
        val switchCompact = findViewById<MaterialSwitch>(R.id.switchCompact)
        val etRatio = findViewById<EditText>(R.id.etCompactRatio)
        val tvSession = findViewById<TextView>(R.id.tvSession)

        // 省token 模式（抄 Reasonix compact_ratio）：默认关，用密钥 token 时自动开
        switchCompact.setOnCheckedChangeListener { _, isChecked ->
            YamiCore.aiSetCompact(isChecked)
        }
        etRatio.setText("0.85")
        etRatio.setOnEditorActionListener { _, actionId, _ ->
            if (actionId == EditorInfo.IME_ACTION_DONE) {
                etRatio.text.toString().toDoubleOrNull()?.let { YamiCore.aiSetCompactRatio(it) }
                true
            } else false
        }

        tvSession.text = if (currentSessionId.isBlank()) getString(R.string.ai_session_current) else currentSessionId
        findViewById<View>(R.id.btnNewSession).setOnClickListener {
            currentSessionId = YamiCore.aiSessionNew()
            tvSession.text = currentSessionId
            Toast.makeText(this, "已新建会话", Toast.LENGTH_SHORT).show()
        }

        findViewById<Button>(R.id.btnAddProvider).setOnClickListener {
            val ok = YamiCore.aiSetProvider(
                Provider(
                    name = name.text.toString().ifBlank { "custom" },
                    base_url = base.text.toString().ifBlank { "https://api.openai.com/v1" },
                    api_key = key.text.toString(),
                    model = model.text.toString().ifBlank { "gpt-4o-mini" },
                    protocol = "openai"
                )
            )
            YamiCore.aiSetActive(name.text.toString().ifBlank { "custom" })
            // 用密钥 token 时直接开启省 token 模式
            if (key.text.toString().isNotBlank()) {
                YamiCore.aiSetCompact(true)
                switchCompact.isChecked = true
                Toast.makeText(this, R.string.ai_compact_on_toast, Toast.LENGTH_SHORT).show()
            }
            Toast.makeText(this, if (ok) "已添加 Provider" else "添加失败", Toast.LENGTH_SHORT).show()
        }

        findViewById<Button>(R.id.btnDeepseekBridge).setOnClickListener {
            val cookies = CookieManager.getInstance().getCookie("https://chat.deepseek.com") ?: ""
            if (cookies.isBlank()) {
                Toast.makeText(this, "请先在浏览器登录 chat.deepseek.com", Toast.LENGTH_LONG).show()
                webView.loadUrl("https://chat.deepseek.com")
                return@setOnClickListener
            }
            val ok = YamiCore.aiSetProvider(
                Provider(name = "deepseek-web", protocol = "deepseek-web", cookies = cookies, model = "deepseek-chat")
            )
            YamiCore.aiSetActive("deepseek-web")
            // 免费白嫖桥不强制省 token（默认关）
            Toast.makeText(this, if (ok) "DeepSeek 白嫖已启用（零 key）" else "启用失败", Toast.LENGTH_SHORT).show()
        }

        findViewById<Button>(R.id.btnAnalyze).setOnClickListener {
            val caps = YamiCore.captures()
            val id = if (caps.isNotEmpty()) caps.last().id.toString() else ""
            Thread {
                val r = YamiCore.aiAnalyze(id, "请分析这条请求：有无 token 泄露/注入风险？给可复制的 curl。")
                runOnUiThread { out.text = r }
            }.start()
        }

        // 有会话的聊天发送（省token：上下文压缩）
        findViewById<View>(R.id.btnAiRun).setOnClickListener {
            val task = etInput.text.toString().trim()
            if (task.isBlank()) return@setOnClickListener
            if (currentSessionId.isBlank()) { currentSessionId = YamiCore.aiSessionNew(); tvSession.text = currentSessionId }
            Thread {
                val r = YamiCore.aiChatSession(currentSessionId, task)
                runOnUiThread {
                    out.text = "你：$task\n\nAI：$r"
                    aiScroll.post { aiScroll.scrollTo(0, out.bottom) }
                }
            }.start()
        }

        // Agent 操控浏览器（也在会话里，省token）
        findViewById<View>(R.id.btnAgentRun).setOnClickListener {
            val task = etInput.text.toString().trim()
            if (task.isBlank()) return@setOnClickListener
            if (currentSessionId.isBlank()) { currentSessionId = YamiCore.aiSessionNew(); tvSession.text = currentSessionId }
            Thread {
                val r = YamiCore.aiAgentRunSession(currentSessionId, task)
                runOnUiThread {
                    out.text = "Agent：$task\n\n$r"
                    aiScroll.post { aiScroll.scrollTo(0, out.bottom) }
                }
            }.start()
        }

        // ---- SSH 会话类型：连接远程主机并在其上执行命令 ----
        val etSshHost = findViewById<EditText>(R.id.etSshHost)
        val etSshUser = findViewById<EditText>(R.id.etSshUser)
        val etSshSecret = findViewById<EditText>(R.id.etSshSecret)
        val switchSshKey = findViewById<MaterialSwitch>(R.id.switchSshKey)
        val tvSshStatus = findViewById<TextView>(R.id.tvSshStatus)
        var sshId = ""
        findViewById<View>(R.id.btnSshConnect).setOnClickListener {
            val host = etSshHost.text.toString().ifBlank { "127.0.0.1:22" }
            val user = etSshUser.text.toString().ifBlank { "root" }
            val auth = if (switchSshKey.isChecked) "key" else "password"
            val secret = etSshSecret.text.toString()
            Thread {
                val id = YamiCore.aiSshConnect(host, user, auth, secret)
                runOnUiThread {
                    if (id.startsWith("err:")) {
                        tvSshStatus.text = id
                        sshId = ""
                    } else {
                        sshId = id
                        tvSshStatus.text = getString(R.string.ssh_connected, id)
                    }
                }
            }.start()
        }
        findViewById<View>(R.id.btnSshExec).setOnClickListener {
            val cmd = etInput.text.toString().trim()
            if (cmd.isBlank()) return@setOnClickListener
            if (sshId.isBlank()) { tvSshStatus.text = "请先连接 SSH"; return@setOnClickListener }
            Thread {
                val r = YamiCore.aiSshExec(sshId, cmd)
                runOnUiThread {
                    out.text = "SSH $sshId \$ $cmd\n$r"
                    aiScroll.post { aiScroll.scrollTo(0, out.bottom) }
                }
            }.start()
        }
    }

    private fun go() {
        var url = etUrl.text.toString().trim()
        if (!url.startsWith("http://") && !url.startsWith("https://")) {
            url = "https://$url"
        }
        webView.loadUrl(url)
    }

    private fun loadHome() {
        webView.loadUrl("file:///android_asset/home.html")
        etUrl.setText("")
    }

    // 浏览器内"抓包为空"引导：证书多半没装
    private fun updateBanner() {
        val empty = YamiCore.captures().isEmpty()
        captureBanner.visibility = if (empty) View.VISIBLE else View.GONE
    }

    private fun showPanel(which: Int) {
        if (which == currentPanel) return
        currentPanel = which
        val panels = arrayOf(flBrowser, flCapture, flToken, flSettings, flAi)
        for (i in panels.indices) {
            val v = panels[i]
            v.visibility = if (i == which) View.VISIBLE else View.GONE
            if (i == which && v.visibility == View.VISIBLE) {
                v.startAnimation(android.view.animation.AnimationUtils.loadAnimation(this, R.anim.panel_fade_in))
            }
        }
        if (which == 0) updateBanner()
        if (which == 1) refreshCapture()
        if (which == 2) refreshToken()
    }

    private fun refreshCapture() {
        val items = YamiCore.captures()
        captureAdapter.setItems(items)
        emptyCapture.visibility = if (items.isEmpty()) View.VISIBLE else View.GONE
        updateStats()
    }

    private fun refreshToken() {
        val items = YamiCore.tokens()
        tokenAdapter.setItems(items)
        emptyToken.visibility = if (items.isEmpty()) View.VISIBLE else View.GONE
    }

    private fun updateStats() {
        val caps = YamiCore.captures()
        val toks = YamiCore.tokens()
        statRequests.text = caps.size.toString()
        statTokens.text = toks.size.toString()
        statLogins.text = caps.count { it.is_login }.toString()
    }

    private fun copyAllTokens() {
        val toks = YamiCore.tokens()
        val text = toks.joinToString("\n") { "${it.key} = ${it.value}" }
        copyText(text, "yami-tokens")
        Toast.makeText(this, getString(R.string.toast_copied, toks.size), Toast.LENGTH_SHORT).show()
    }

    private fun installCa() {
        val pem = YamiCore.caPEM()
        if (pem.isEmpty()) {
            Toast.makeText(this, "引擎未启动", Toast.LENGTH_SHORT).show()
            return
        }
        val pemBytes = pem.toByteArray(Charsets.UTF_8)
        // 主路径：系统证书安装器（API 24+），用户命名后即装好为用户 CA
        try {
            val intent = Intent("android.credentials.INSTALL")
            intent.putExtra("android.credentials.CERTIFICATE", pemBytes)
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            startActivity(intent)
            Toast.makeText(this, R.string.toast_ca_installing, Toast.LENGTH_LONG).show()
            return
        } catch (_: Exception) {
            // 该 intent 不可用，走文件兜底
        }
        // 兜底：写出 .crt 并用 FileProvider 打开证书安装器
        try {
            val dir = File(getExternalFilesDir(null), "certs")
            dir.mkdirs()
            val file = File(dir, "yami-UA-CA.crt")
            file.writeText(pem)
            val uri = FileProvider.getUriForFile(this, "$packageName.fileprovider", file)
            val view = Intent(Intent.ACTION_VIEW)
            view.setDataAndType(uri, "application/x-x509-ca-cert")
            view.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            startActivity(view)
            Toast.makeText(this, getString(R.string.toast_ca_install_failed, file.absolutePath), Toast.LENGTH_LONG).show()
        } catch (e: Exception) {
            Toast.makeText(this, "证书安装失败：${e.message}", Toast.LENGTH_LONG).show()
        }
    }

    private fun copyText(text: String, label: String) {
        val cm = getSystemService(CLIPBOARD_SERVICE) as ClipboardManager
        cm.setPrimaryClip(ClipData.newPlainText(label, text))
    }

    // 导出全部抓包为 HAR（抓包增强：ProxyPin 差距补全）。长按"清空"触发。
    private fun exportHAR() {
        val har = YamiCore.aiExportHAR()
        if (har.isEmpty() || har.startsWith("err:")) {
            Toast.makeText(this, "HAR 导出失败：$har", Toast.LENGTH_LONG).show()
            return
        }
        try {
            val dir = File(getExternalFilesDir(null), "har")
            dir.mkdirs()
            val file = File(dir, "yami-capture-${System.currentTimeMillis()}.har")
            file.writeText(har)
            val uri = FileProvider.getUriForFile(this, "$packageName.fileprovider", file)
            val share = Intent(Intent.ACTION_SEND)
            share.type = "application/json"
            share.putExtra(Intent.EXTRA_STREAM, uri)
            share.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            startActivity(Intent.createChooser(share, "导出 HAR"))
        } catch (e: Exception) {
            Toast.makeText(this, "HAR 导出失败：${e.message}", Toast.LENGTH_LONG).show()
        }
    }

    override fun onBackPressed() {
        when {
            currentPanel != 0 -> { showPanel(0); bottomNav.selectedItemId = R.id.nav_browser }
            webView.canGoBack() -> webView.goBack()
            else -> super.onBackPressed()
        }
    }
}

/** Card-style adapter for captured requests. */
class CaptureAdapter : RecyclerView.Adapter<CaptureAdapter.VH>() {
    private var items: List<Record> = emptyList()
    fun setItems(list: List<Record>) { items = list; notifyDataSetChanged() }

    class VH(view: View) : RecyclerView.ViewHolder(view) {
        val tvMethod: TextView = view.findViewById(R.id.tvMethod)
        val tvStatus: TextView = view.findViewById(R.id.tvStatus)
        val tvScheme: TextView = view.findViewById(R.id.tvScheme)
        val tvUrl: TextView = view.findViewById(R.id.tvUrl)
        val tvMeta: TextView = view.findViewById(R.id.tvMeta)
        val dotLogin: View = view.findViewById(R.id.dotLogin)
    }

    override fun onCreateViewHolder(parent: android.view.ViewGroup, viewType: Int): VH {
        val v = LayoutInflater.from(parent.context).inflate(R.layout.item_capture, parent, false)
        return VH(v)
    }

    override fun onBindViewHolder(holder: VH, position: Int) {
        val r = items[position]
        holder.tvMethod.text = r.method
        holder.tvStatus.text = r.status_code.toString()
        holder.tvScheme.text = if (r.is_https) "HTTPS" else "HTTP"
        holder.tvUrl.text = r.url
        holder.tvMeta.text = "${r.host} · token ${r.tokens.size}"
        holder.dotLogin.visibility = if (r.is_login) View.VISIBLE else View.GONE
        holder.itemView.setOnClickListener {
            val detail = buildString {
                append("${r.method} ${r.url}\n")
                append("status: ${r.status_code}\n")
                append("tokens: ${r.tokens.size}\n")
                r.tokens.forEach { append("  ${it.key} = ${it.value}\n") }
            }
            val cm = holder.itemView.context.getSystemService(CLIPBOARD_SERVICE) as ClipboardManager
            cm.setPrimaryClip(ClipData.newPlainText("yami-request", detail))
            Toast.makeText(holder.itemView.context, "已复制请求详情", Toast.LENGTH_SHORT).show()
        }
    }

    override fun getItemCount() = items.size
}

/** Card-style adapter for extracted tokens. */
class TokenAdapter : RecyclerView.Adapter<TokenAdapter.VH>() {
    private var items: List<Token> = emptyList()
    fun setItems(list: List<Token>) { items = list; notifyDataSetChanged() }

    class VH(view: View) : RecyclerView.ViewHolder(view) {
        val tvKey: TextView = view.findViewById(R.id.tvKey)
        val tvSource: TextView = view.findViewById(R.id.tvSource)
        val tvValue: TextView = view.findViewById(R.id.tvValue)
        val tvLoginFlag: TextView = view.findViewById(R.id.tvLoginFlag)
    }

    override fun onCreateViewHolder(parent: android.view.ViewGroup, viewType: Int): VH {
        val v = LayoutInflater.from(parent.context).inflate(R.layout.item_token, parent, false)
        return VH(v)
    }

    override fun onBindViewHolder(holder: VH, position: Int) {
        val t = items[position]
        holder.tvKey.text = t.key
        holder.tvSource.text = t.source
        holder.tvValue.text = t.value
        holder.tvLoginFlag.visibility = if (t.is_login) View.VISIBLE else View.GONE
        holder.itemView.setOnClickListener {
            val cm = holder.itemView.context.getSystemService(CLIPBOARD_SERVICE) as ClipboardManager
            cm.setPrimaryClip(ClipData.newPlainText(t.key, t.value))
            Toast.makeText(holder.itemView.context, "已复制 ${t.key}", Toast.LENGTH_SHORT).show()
        }
    }

    override fun getItemCount() = items.size
}
