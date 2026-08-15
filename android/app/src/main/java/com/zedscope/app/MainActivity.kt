package com.zedscope.app

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
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
import androidx.webkit.ProxyController
import java.io.File
import android.util.Log

class MainActivity : AppCompatActivity() {

    private lateinit var webView: WebView
    private lateinit var etUrl: EditText
    private lateinit var etInput: EditText   // AI 输入：setupAiPanel 赋值，runAgentTask 等成员函数也用
    private lateinit var tvSession: TextView   // 会话 ID 展示：setupAiPanel 赋值，runAgentTask 也用
    private lateinit var bottomNav: BottomNavigationView
    private lateinit var flBrowser: android.widget.FrameLayout
    private lateinit var flCapture: android.widget.FrameLayout
    private lateinit var flToken: android.widget.FrameLayout
    private lateinit var flSettings: android.widget.FrameLayout
    private lateinit var flAi: android.widget.FrameLayout
    private lateinit var skeletonCapture: android.widget.FrameLayout
    private lateinit var skeletonToken: android.widget.FrameLayout
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

    // 对话式 AI 面板
    private lateinit var chatAdapter: ChatAdapter
    private lateinit var rvChat: RecyclerView
    private lateinit var tvChatEmpty: TextView
    private lateinit var chipTasks: com.google.android.material.chip.ChipGroup
    private lateinit var typingDots: com.zedscope.app.ui.TypingDots
    private val chatHandler = Handler(Looper.getMainLooper())

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
        boot("setContentView")

        maybeShowLastCrash()

        webView = findViewById(R.id.webview)
        etUrl = findViewById(R.id.etUrl)
        bottomNav = findViewById(R.id.bottomNav)
        flBrowser = findViewById(R.id.flBrowser)
        flCapture = findViewById(R.id.flCapture)
        flToken = findViewById(R.id.flToken)
        flSettings = findViewById(R.id.flSettings)
        flAi = findViewById(R.id.flAi)
        skeletonCapture = findViewById(R.id.skeletonCapture)
        skeletonToken = findViewById(R.id.skeletonToken)
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

        setupWebView()
        setupPanels()
        setupSettings()

        val startRes = YamiCore.start()
        boot("YamiCore.start=$startRes")
        if (startRes == "ok") {
            YamiCore.setBodyDir(cacheDir.absolutePath + "/yami-bodies")
            YamiCore.setCleanCapture(true) // clean capture ON by default
            applyWebViewProxy()
            Toast.makeText(this, R.string.toast_proxy_started, Toast.LENGTH_SHORT).show()
        } else {
            // 引擎启动失败（端口占用/Go 侧错误等）：直接暴露原因，而不是静默继续，
            // 方便用户反馈。注意 Go panic 走 SIGABRT 时 Kotlin 抓不到，会表现为闪退，
            // 那种情况需要 adb logcat 才能定位（见仓库 README 排错表）。
            Toast.makeText(this, "引擎启动失败：$startRes", Toast.LENGTH_LONG).show()
        }

        if (YamiCore.aiStart()) {
            aiBridge = AiBridge(webView)
            aiBridge.start()
            boot("aiStart=ok")
        } else {
            boot("aiStart=failed")
        }

        setupAiPanel()
        loadHome()
        boot("UI ready")
    }

    /**
     * 若上次运行留下崩溃日志(cacheDir/crash.log，由 YamiApplication 的全局
     * 未捕获异常处理器写入)，提示用户反馈。注意 native 崩溃(Go panic->SIGABRT)
     * Kotlin 层抓不到，此提示主要针对 Kotlin 层异常。
     */
    private fun maybeShowLastCrash() {
        try {
            val f = File(cacheDir, "crash.log")
            if (f.exists() && f.length() > 0) {
                Toast.makeText(this, "检测到上次 Kotlin 崩溃日志(crash.log)，请反馈以便定位", Toast.LENGTH_LONG).show()
                return
            }
            // 没有 Kotlin 崩溃日志，但启动轨迹若未到达"UI ready"，说明上次在更早阶段
            // 就 native 崩了（Go panic->SIGABRT，Kotlin 抓不到）。把最后到达的阶段回显，
            // 用户无需 adb 即可把这段信息反馈过来定位。
            val boot = File(cacheDir, "boot.log")
            if (boot.exists() && boot.length() > 0) {
                val last = boot.readLines().filter { it.isNotBlank() }.lastOrNull() ?: ""
                if (!last.contains("UI ready")) {
                    Toast.makeText(this, "上次启动止于：$last（native 崩溃？请反馈此信息）", Toast.LENGTH_LONG).show()
                }
            }
        } catch (_: Throwable) {
        }
    }

    /** 写一行启动轨迹到 cacheDir/boot.log（每次落盘，闪退前最后一行即死因线索）。 */
    private fun boot(stage: String) {
        try {
            File(cacheDir, "boot.log").appendText("${System.currentTimeMillis()} $stage\n")
        } catch (_: Throwable) {
        }
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
                if (u.startsWith("yami://ai")) { showPanel(4); return true }
                if (u.startsWith("yami://settings")) { showPanel(3); return true }
                view?.loadUrl(u)
                return true
            }
            override fun onPageStarted(view: WebView?, url: String?, favicon: android.graphics.Bitmap?) {
                if (url != null && url.startsWith("http")) etUrl.setText(url)
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
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            Toast.makeText(this, "代理拦截需要 Android 10+", Toast.LENGTH_LONG).show()
            return
        }
        // Verify the capture engine is ACTUALLY listening before routing the
        // WebView through it. If the engine failed to bind (port in use etc.)
        // the app would otherwise point the browser at a dead proxy and every
        // request fails — "browser completely unusable". On probe failure we
        // fall back to DIRECT browsing and tell the user.
        if (!tcpReachable(YamiCore.proxyAddr())) {
            Toast.makeText(this, "抓包引擎未就绪(端口不可达)，已跳过代理，浏览器直连可用", Toast.LENGTH_LONG).show()
            return
        }
        try {
            val config = ProxyConfig.Builder()
                .addProxyRule(YamiCore.proxyAddr())
                .build()
            val executor = java.util.concurrent.Executors.newSingleThreadExecutor()
            androidx.webkit.ProxyController.getInstance()
                .setProxyOverride(config, executor, java.lang.Runnable { })
        } catch (e: Throwable) {
            // Proxy override is best-effort; never let it crash startup.
            e.printStackTrace()
        }
    }

    /** 尝试 TCP 连接 addr(host:port)，可连返回 true（探测引擎是否真的在监听）。 */
    private fun tcpReachable(hostPort: String): Boolean {
        return try {
            val host = hostPort.substringBefore(":").ifBlank { "127.0.0.1" }
            val port = hostPort.substringAfter(":").toIntOrNull() ?: return false
            val s = java.net.Socket()
            try {
                s.connect(java.net.InetSocketAddress(host, port), 500)
                true
            } finally {
                try { s.close() } catch (_: Throwable) {}
            }
        } catch (_: Throwable) {
            false
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
            Toast.makeText(this, "已清空", Toast.LENGTH_SHORT).show()
        }
        // 长按"清空"导出 HAR（抓包增强：ProxyPin 差距补全）
        findViewById<Button>(R.id.btnClear).setOnLongClickListener {
            exportHAR()
            true
        }


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
        etInput = findViewById<EditText>(R.id.etAiInput)
        val switchCompact = findViewById<MaterialSwitch>(R.id.switchCompact)
        val etRatio = findViewById<EditText>(R.id.etCompactRatio)
        tvSession = findViewById<TextView>(R.id.tvSession)

        // 对话气泡列表 + 快捷任务模板
        rvChat = findViewById(R.id.rvChat)
        tvChatEmpty = findViewById(R.id.tvChatEmpty)
        typingDots = findViewById(R.id.typingDots)
        chipTasks = findViewById(R.id.chipTasks)
        chatAdapter = ChatAdapter()
        rvChat.layoutManager = LinearLayoutManager(this)
        rvChat.itemAnimator = null
        rvChat.adapter = chatAdapter
        updateChatEmpty()
        buildTaskChips()

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
            chatAdapter.clear()
            updateChatEmpty()
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
            chatAdapter.add(ChatMsg("user", "分析最近一条抓包（token 泄露 / 注入风险 + 可复制 curl）"))
            updateChatEmpty()
            showTyping()
            Thread {
                val r = YamiCore.aiAnalyze(id, "请分析这条请求：有无 token 泄露/注入风险？给可复制的 curl。")
                runOnUiThread {
                    hideTyping()
                    chatAdapter.add(ChatMsg("ai", ""))
                    streamInto(r.ifBlank { "（暂无抓包数据，先去浏览器访问网页）" })
                }
            }.start()
        }

        // 有会话的聊天发送（省token：上下文压缩）
        findViewById<View>(R.id.btnAiRun).setOnClickListener {
            val task = etInput.text.toString().trim()
            if (task.isBlank()) return@setOnClickListener
            if (currentSessionId.isBlank()) { currentSessionId = YamiCore.aiSessionNew(); tvSession.text = currentSessionId }
            etInput.setText("")
            chatAdapter.add(ChatMsg("user", task))
            updateChatEmpty()
            showTyping()
            Thread {
                val r = YamiCore.aiChatSession(currentSessionId, task)
                runOnUiThread {
                    hideTyping()
                    chatAdapter.add(ChatMsg("ai", ""))
                    streamInto(r.ifBlank { "（AI 未返回内容）" })
                }
            }.start()
        }

        // Agent 操控浏览器（也在会话里，省token）
        findViewById<View>(R.id.btnAgentRun).setOnClickListener {
            runAgentTask(etInput.text.toString().trim())
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
            chatAdapter.add(ChatMsg("user", "SSH $sshId \$ $cmd"))
            updateChatEmpty()
            showTyping()
            Thread {
                val r = YamiCore.aiSshExec(sshId, cmd)
                runOnUiThread {
                    hideTyping()
                    chatAdapter.add(ChatMsg("ai", ""))
                    streamInto(r.ifBlank { "（无输出）" })
                }
            }.start()
        }
    }

    private fun runAgentTask(task: String) {
        if (task.isBlank()) return
        if (currentSessionId.isBlank()) { currentSessionId = YamiCore.aiSessionNew(); tvSession.text = currentSessionId }
        etInput.setText("")
        chatAdapter.add(ChatMsg("user", task))
        updateChatEmpty()
        showTyping()
        Thread {
            val r = YamiCore.aiAgentRunSession(currentSessionId, task)
            runOnUiThread {
                hideTyping()
                chatAdapter.add(ChatMsg("ai", ""))
                streamInto(r.ifBlank { "（Agent 未返回内容）" })
            }
        }.start()
    }

    /** 打字机式流式：把完整文本逐帧写入最后一个 AI 气泡。 */
    private fun streamInto(full: String) {
        if (full.isBlank()) { chatAdapter.updateLast("（无内容）"); return }
        chatHandler.removeCallbacksAndMessages(null)
        var shown = 0
        val step = 4
        val runnable = object : Runnable {
            override fun run() {
                shown = minOf(full.length, shown + step)
                chatAdapter.updateLast(full.substring(0, shown))
                rvChat.scrollToPosition(chatAdapter.itemCount - 1)
                if (shown < full.length) chatHandler.postDelayed(this, 18)
            }
        }
        chatHandler.post(runnable)
    }

    private fun updateChatEmpty() {
        val empty = chatAdapter.isEmpty()
        tvChatEmpty.visibility = if (empty) View.VISIBLE else View.GONE
        rvChat.visibility = if (empty) View.GONE else View.VISIBLE
    }

    private fun buildTaskChips() {
        val templates = listOf(
            "打开目标网站并登录，把登录后的 token 复制给我",
            "分析最近一条抓包：有无 token 泄露 / 注入风险，给可复制的 curl",
            "在当前页面找到所有表单并自动填入测试数据",
            "总结当前打开的网页的核心内容"
        )
        chipTasks.removeAllViews()
        templates.forEach { t ->
            val chip = com.google.android.material.chip.Chip(this).apply {
                text = t
                isClickable = true
                setOnClickListener {
                    etInput.setText(t)
                    runAgentTask(t)
                }
            }
            chipTasks.addView(chip)
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
        if (which == 1) loadWithSkeleton(skeletonCapture, ::refreshCapture)
        if (which == 2) loadWithSkeleton(skeletonToken, ::refreshToken)
    }

    /**
     * Show a precise skeleton, then run [load] after a short beat and crossfade
     * the skeleton out — the GitHub-Android-style "precise skeleton" load.
     */
    private fun loadWithSkeleton(skeleton: android.widget.FrameLayout, load: () -> Unit) {
        skeleton.alpha = 1f
        skeleton.visibility = View.VISIBLE
        skeleton.postDelayed({
            load()
            skeleton.animate().alpha(0f).setDuration(180L).withEndAction {
                skeleton.visibility = View.GONE
            }.start()
        }, 280L)
    }

    /** Show the AI "thinking" dots while a response streams in. */
    private fun showTyping() {
        typingDots.visibility = View.VISIBLE
        typingDots.start()
    }

    private fun hideTyping() {
        typingDots.stop()
        typingDots.visibility = View.GONE
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
            Toast.makeText(this, "引擎未启动，无法导出证书", Toast.LENGTH_LONG).show()
            return
        }
        // 0) 先把证书写到 App 外部文件目录（用户可用 MT管理器/文件管理器访问），
        //    并复制路径到剪贴板——即使系统安装器不弹，用户也知道证书在哪。
        val certDir = File(getExternalFilesDir(null), "certs")
        certDir.mkdirs()
        val certFile = File(certDir, "ZedScope-CA.crt")
        certFile.writeText(pem)
        copyText(certFile.absolutePath, "zedscope-ca-path")
        val pemBytes = pem.toByteArray(Charsets.UTF_8)
        // 1) 主路径：系统证书安装器（API 24+），EXTRA_CERTIFICATE 必须传 DER 二进制
        try {
            val der = pemToDer(pemBytes)
            val intent = Intent("android.credentials.INSTALL")
            intent.putExtra("android.credentials.CERTIFICATE", der)
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            startActivity(intent)
            Toast.makeText(this,
                "证书路径已复制：\n${certFile.absolutePath}",
                Toast.LENGTH_LONG).show()
            return
        } catch (_: Exception) {
            // 该 intent 不可用，走文件兜底
        }
        // 2) 兜底：FileProvider 打开 .crt（系统能正确解析 PEM 文件）
        try {
            val uri = FileProvider.getUriForFile(this, "$packageName.fileprovider", certFile)
            val view = Intent(Intent.ACTION_VIEW)
            view.setDataAndType(uri, "application/x-x509-ca-cert")
            view.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            startActivity(view)
            Toast.makeText(this, "证书文件：${certFile.absolutePath}（路径已复制）", Toast.LENGTH_LONG).show()
        } catch (e: Exception) {
            Toast.makeText(this, "自动安装失败，证书在：${certFile.absolutePath}（路径已复制）", Toast.LENGTH_LONG).show()
        }
    }

    /** 把 PEM 文本字节转成 DER 二进制（供 EXTRA_CERTIFICATE 使用）。 */
    private fun pemToDer(pemBytes: ByteArray): ByteArray {
        val s = String(pemBytes, Charsets.UTF_8)
        val b64 = s
            .replace("-----BEGIN CERTIFICATE-----", "")
            .replace("-----END CERTIFICATE-----", "")
            .replace(Regex("\\s"), "")
        return android.util.Base64.decode(b64, android.util.Base64.DEFAULT)
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

