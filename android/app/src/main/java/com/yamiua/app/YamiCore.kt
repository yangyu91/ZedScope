package com.yamiua.app

import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import yami.Yami

/**
 * Thin Kotlin wrapper around the gomobile-generated `yami.Yami` binding.
 * All heavy lifting (MITM, capture, token extraction, modify) happens in Go.
 */
data class Token(
    val key: String = "",
    val value: String = "",
    val source: String = "",
    val url: String = "",
    val is_login: Boolean = false,
    val captured_at: Long = 0
)

data class Record(
    val id: Long = 0,
    val method: String = "",
    val url: String = "",
    val host: String = "",
    val status_code: Int = 0,
    val is_https: Boolean = false,
    val is_login: Boolean = false,
    val time: String = "",
    val tokens: List<Token> = emptyList()
)

data class Provider(
    val name: String = "",
    val base_url: String = "",
    val api_key: String = "",
    val model: String = "",
    val protocol: String = "openai",
    val upstream_proxy: String = "",
    val cookies: String = "",
    val healthy: Boolean = true
)

object YamiCore {
    private val gson = Gson()

    private const val PROXY_ADDR = "127.0.0.1:8899"
    private const val API_ADDR = "127.0.0.1:8900"
    private const val AI_ADDR = "127.0.0.1:8910"

    fun proxyAddr() = PROXY_ADDR
    fun apiAddr() = API_ADDR
    fun aiAddr() = AI_ADDR

    fun start(): Boolean {
        return try {
            Yami.start(PROXY_ADDR, API_ADDR) == "ok"
        } catch (e: Throwable) {
            e.printStackTrace()
            false
        }
    }

    fun caPEM(): String = try { Yami.caPEM() } catch (e: Throwable) { "" }

    fun captures(): List<Record> {
        return try {
            val type = object : TypeToken<List<Record>>() {}.type
            gson.fromJson<List<Record>>(Yami.captures(), type) ?: emptyList()
        } catch (e: Throwable) {
            emptyList()
        }
    }

    fun tokens(): List<Token> {
        return try {
            val type = object : TypeToken<List<Token>>() {}.type
            gson.fromJson<List<Token>>(Yami.tokens(), type) ?: emptyList()
        } catch (e: Throwable) {
            emptyList()
        }
    }

    fun clear() {
        try { Yami.clear() } catch (_: Throwable) {}
    }

    // Toggle the default clean-capture filter (hides yami's own / AI / relay traffic).
    fun setCleanCapture(on: Boolean): Boolean = try {
        Yami.setCleanCapture(on) == "ok"
    } catch (e: Throwable) { false }

    // ---------------- AI relay / agent ----------------

    fun aiStart(): Boolean = try { Yami.aiStart(AI_ADDR) == "ok" } catch (e: Throwable) { false }

    fun aiSetProvider(p: Provider): Boolean = try {
        val json = gson.toJson(mapOf(
            "name" to p.name, "base_url" to p.base_url, "api_key" to p.api_key,
            "model" to p.model, "protocol" to p.protocol,
            "upstream_proxy" to p.upstream_proxy, "cookies" to p.cookies
        ))
        Yami.aiSetProvider(json) == "ok"
    } catch (e: Throwable) { false }

    fun aiSetActive(name: String): Boolean = try { Yami.aiSetActive(name) == "ok" } catch (_: Throwable) { false }

    fun aiListProviders(): List<Provider> = try {
        val type = object : TypeToken<List<Provider>>() {}.type
        gson.fromJson<List<Provider>>(Yami.aiListProviders(), type) ?: emptyList()
    } catch (e: Throwable) { emptyList() }

    fun aiChat(prompt: String): String = try { Yami.aiChat(prompt) } catch (e: Throwable) { "err: ${e.message}" }

    // 省token 模式（抄 Reasonix compact_ratio）
    fun aiSetCompact(on: Boolean): Boolean = try { Yami.aiSetCompact(on) == "ok" } catch (e: Throwable) { false }
    fun aiSetCompactRatio(r: Double): Boolean = try { Yami.aiSetCompactRatio(r) == "ok" } catch (e: Throwable) { false }

    // 会话管理
    fun aiSessionNew(): String = try { Yami.aiSessionNew() } catch (e: Throwable) { "" }
    fun aiSessionList(): List<String> = try {
        val type = object : TypeToken<List<String>>() {}.type
        gson.fromJson<List<String>>(Yami.aiSessionList(), type) ?: emptyList()
    } catch (e: Throwable) { emptyList() }
    fun aiSessionClear(id: String): Int = try { Yami.aiSessionClear(id).toIntOrNull() ?: 0 } catch (e: Throwable) { 0 }

    // 有会话的聊天（带 X-Yami-Session 走压缩层）
    fun aiChatSession(sessionId: String, prompt: String): String =
        try { Yami.aiChatSession(sessionId, prompt) } catch (e: Throwable) { "err: ${e.message}" }

    fun aiAnalyze(captureId: String, prompt: String): String = try {
        Yami.aiAnalyze(captureId, prompt)
    } catch (e: Throwable) { "err: ${e.message}" }

    fun aiAgentRun(task: String): String = try { Yami.aiAgentRun(task) } catch (e: Throwable) { "err: ${e.message}" }

    // Agent 跑在会话里（省token）
    fun aiAgentRunSession(sessionId: String, task: String): String =
        try { Yami.aiAgentRunSession(sessionId, task) } catch (e: Throwable) { "err: ${e.message}" }

    // ---------------- 抓包增强（ProxyPin 差距补全） ----------------

    /** 安装抓包增强规则（域名过滤 / 拦截 / 映射 / AES 解密），入参 RulesConfig JSON。 */
    fun aiSetRules(json: String): Boolean = try { Yami.aiSetRules(json) == "ok" } catch (e: Throwable) { false }
    /** 读取当前抓包增强规则（RulesConfig JSON）。 */
    fun aiGetRules(): String = try { Yami.aiGetRules() } catch (e: Throwable) { "{}" }
    /** 导出全部抓包为 HAR JSON 文档。 */
    fun aiExportHAR(): String = try { Yami.aiExportHAR() } catch (e: Throwable) { "err: ${e.message}" }
    /** 按 keyword/status/content_type 搜索抓包，返回 Record JSON 数组。 */
    fun aiSearchCaptures(json: String): String = try { Yami.aiSearchCaptures(json) } catch (e: Throwable) { "[]" }

    // ---------------- 协议 / 会话 / Agent 增强 ----------------

    /** Provider 健康 + active 状态（JSON 数组：{name,healthy,active}）。 */
    fun aiProviderHealth(): String = try { Yami.aiProviderHealth() } catch (e: Throwable) { "[]" }
    /** 导出某个会话为 JSON（供备份 / 跨端迁移）。 */
    fun aiSessionExport(id: String): String = try { Yami.aiSessionExport(id) } catch (e: Throwable) { "" }
    /** 导入会话 JSON（AiSessionExport 的逆向）。 */
    fun aiSessionImport(id: String, json: String): Boolean = try { Yami.aiSessionImport(id, json) == "ok" } catch (e: Throwable) { false }
    /** 解析 vmess/vless/trojan/ss/socks5 分享链接为 xray outbound（JSON）。 */
    fun aiProtoParse(link: String): String = try { Yami.aiProtoParse(link) } catch (e: Throwable) { "err: ${e.message}" }
    /** Agent 可用动作 schema（JSON 数组），供 UI 展示。 */
    fun aiAgentActions(): String = try { Yami.aiAgentActions() } catch (e: Throwable) { "[]" }
    /** 切换 Agent 离线任务规划器。 */
    fun aiSetPlanner(on: Boolean): Boolean = try { Yami.aiSetPlanner(on) == "ok" } catch (e: Throwable) { false }

    // Browser command queue (driven by AiBridge on a background thread).
    fun aibrowserPending(): String = try { Yami.aiBrowserPending() } catch (e: Throwable) { "" }
    fun aibrowserComplete(id: String, result: String) {
        try { Yami.aiBrowserComplete(id, result) } catch (_: Throwable) {}
    }
}
