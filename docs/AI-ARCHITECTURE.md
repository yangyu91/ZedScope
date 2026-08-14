# yami-UA · 全闭环 AI 浏览器架构（Roadmap）

> 目标：做一款**全端互通、安全绿色、免费好用、强大开源、优雅**的 AI 浏览器。
> 安卓版**无需高级权限**即可工作；root / Shizuku 仅作为可选增强。

---

## 0. 一句话闭环

```
浏览器(WebView) ──流量──▶ 内置 MITM 抓包代理(127.0.0.1)
        ▲                      │ 捕获 / 改包 / Token 提取
        │   AI 指令(JSON)       ▼
   AI Agent 循环 ◀──── 抓包数据 ──▶ 内置 AI 中转站(OpenAI 兼容)
        │                              │ 多 Provider + 故障转移
        │   cookie(网页登录态)          ▼
        └──── 驱动页面(JS Bridge)    DeepSeek 网页白嫖桥 / 任意 OpenAI 后端
                                     （全部流量可走代理）
```

**完全免费白嫖**：DeepSeek 桥复用你在 WebView 里已登录的 chat.deepseek.com 会话（cookie），
不花一分钱 API 费；中转站把对话转成 OpenAI 协议，喂给"抓包分析师 / 浏览器操作员"两个智能体；
AI 的指令通过 WebView 自身的 JS Bridge 执行——**因为浏览器在自己页面里跑指令，所以不需要任何安卓权限**。

---

## 1. 四个参考仓库 → yami-UA 的落点

| 参考仓库 | 它的核心 | yami-UA 对应实现 |
|---|---|---|
| [farion1231/cc-switch](https://github.com/farion1231/cc-switch) | 本地代理 + 多 Provider + 协议转换 + 故障转移 + OpenAI 兼容 | `go/ai` 中转站：`/v1/chat/completions`、多 Provider 路由、健康探测、自动 failover、代理感知客户端 |
| [zhu1090093659/deepseek-pp](https://github.com/zhu1090093659/deepseek-pp) | 浏览器扩展，复用 DeepSeek **网页登录态**做免费 Agent | `go/ai/deepseek_web.go`：导出 WebView 的 cookie → 驱动 chat.deepseek.com，零 key 免费 |
| [TensorHub-ORG/Coomi-Android](https://github.com/TensorHub-ORG/Coomi-Android) | 本地优先 Agent 工作台，工具循环驱动设备/浏览器 | `go/ai/agent.go`：工具调用循环（navigate/click/type/extract/analyze/copy_token） |
| v2ray / var2proxy（多协议代理） | vmess/vless/trojan/ss/socks5 全局代理 | `go/proxy/manager.go`：分享链接解析 → xray 配置；MITM 支持上游代理，浏览全走代理 |
| [esengine/DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) | content-driven 上下文压缩（稳定前缀 + 滚动摘要 + 尾巴） | `go/ai/session.go`：`SessionStore.Compact` 按 `compact_ratio` 压缩长会话，省 token 不爆窗 |

---

## 2. 模块设计

### 2.1 内置 AI 中转站（`go/ai`）
- **入口**：`127.0.0.1:8910/v1/chat/completions`（与 OpenAI 完全兼容，支持 SSE 流式）。
- **多 Provider**：`openai` / `anthropic` / `deepseek-web` 三种协议。
- **故障转移**：`Registry.Probe()` 探活，失败自动切下一个；`complete()` 按候选顺序重试。
- **代理感知**：`DefaultUpstreamProxy` 默认指向 yami 自己的 MITM 监听地址——**AI 后端的请求也走抓包代理**，因此"全程流量可审计、可走用户代理"。
- **可插拔**：`AISetProvider(json)` 注入任意 OpenAI 兼容端点（含社区中继、SiliconFlow、本地 Ollama 等）。

### 2.2 DeepSeek 网页白嫖桥（`deepseek_web.go`）
- 用户提供在 WebView 登录 chat.deepseek.com 后的 cookie（`AISetProvider` 的 `cookies` 字段）。
- 中转站 `POST https://chat.deepseek.com/api/v0/chat/completion`，解析其 SSE 流（answer 在 `choices[].delta.content`，思考在 `v`）。
- 不逆向私有协议、不存储密码，纯复用浏览器已有会话——与 deepseek-pp 同一思路。

### 2.3 抓包 AI 分析（`relay.analyze` + `prompts.go`）
- 系统提示词 `SystemPromptCaptureAnalyst`：识别 token/cookie/鉴权头、SQLi/XSS/泄露风险、给出可复制 curl。
- `/ai/analyze`：传 `capture_id` + 问题 → 把该请求的 头/体 作为上下文喂给 LLM。

### 2.4 无权限浏览器操作 Agent（`agent.go` + 安卓 `AiBridge.kt`）
- `Agent.Run(task)`：工具调用循环。AI 返回 `browser_navigate/click/type/extract` 或 `analyze_capture/copy_token`。
- Go 侧 `browserAdapter` 把指令入队（`AIBrowserPending`），安卓侧后台线程取走，用 `WebView.evaluateJavascript` 在页面内执行，结果回传（`AIBrowserComplete`）。
- **为什么无需权限**：指令在 WebView 自身页面上下文里跑（等价于你在页面里敲 JS），不碰系统 API。

### 2.5 多协议代理（`proxy/manager.go` + `proxy.go`）
- `ParseShareLink`：vmess/vless/trojan/ss → xray outbound。
- `WriteXrayConfig` + `LaunchCore`：生成并拉起 xray/v2ray 核（二进制由用户自带，yami 不捆绑）。
- `Proxy.SetUpstream`：MITM 把流量转发到用户代理，实现"浏览全走代理"。

### 2.6 省 token 模式（上下文压缩，`session.go` + `relay.go`）
- 借鉴 [esengine/DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) 的 **content-driven context maintenance**：稳定前缀 + 一个 rolling summary + 近期尾部。
- `SessionStore.Compact`：当 `estimateTokens ≥ budget(60000) × compact_ratio(0.85)`，把会话压成 `system + 首条 user（稳定前缀，命中 prefix-cache）` + `中间消息经活跃 Provider 摘要` + `末 6 条原文（尾巴）`；摘要失败自动降级为「每条前 200 字符」抽取式兜底，绝不 panic。
- 默认策略（`ApplyDefaultCompact`）：真实密钥 Provider（`openai`/`anthropic` 且 `api_key≠""`）自动开压缩；`deepseek-web` 免费桥默认关。
- 接口：`/ai/compact`、`/ai/compact_ratio`、`/ai/session/new|list|clear`，安卓 AI 面板已接线（开关 + ratio 输入 + 会话卡片）。
- 测试：`session_test.go` 6 用例全绿（压缩后条数 42→9、尾巴原样、降级不崩、关闭 no-op 等）。

### 2.7 抓包增强（借鉴 ProxyPin，`proxy/rules.go` + `search.go` + `mapping.go` + `block.go` + `har.go`）
- 域名白/黑名单过滤（capture-time）、请求拦截（URL 正则命中返回 403 不转发）、请求映射（命中返回本地响应）、AES 解密规则（best-effort 展示明文）、HAR 导出、按关键字/状态/类型搜索。
- 收口：`/api/rules`、`/api/har`、`/api/search` + `YamiCore.aiSetRules/aiExportHAR/aiSearchCaptures`。

### 2.8 多 Provider 健康探测 + 故障转移（借鉴 cc-switch，`ai/provider.go` + `relay.go`）
- `Registry.Probe()` 后台健康探测；`Relay.complete` 按 `candidateOrder()` 做 sticky 故障转移（跳过 unhealthy，失败标记 unhealthy）。
- 收口：`/ai/providers` + `YamiCore.aiProviderHealth`。

### 2.9 会话导出 / 协议解析（收口接口）
- 会话：`Session.ExportJSON/ImportJSON` + `SessionStore.AppendJSONL/LoadJSONL`；收口 `/ai/session/export|import` + `YamiCore.aiSessionExport/Import`。
- 协议：`proxy/proto` 子包（vmess/vless/trojan/ss/socks5）全单测；收口 `YamiCore.aiProtoParse`。
- Agent 动作清单：收口 `/ai/agent/actions` + `YamiCore.aiAgentActions`。

---

## 3. 权限模型

| 能力 | 是否需要权限 | 说明 |
|---|---|---|
| 抓包（本 App WebView） | 否 | `ProxyController` 仅代理本应用，Android 10+ |
| AI 分析 / 聊天 | 否 | 纯本地 + 网络请求 |
| AI 驱动浏览器 | 否 | WebView JS Bridge 页面内执行 |
| 全局代理（其它 App 也走） | 需 Shizuku / root | 用 VpnService 或 iptables；可选增强 |
| 导出系统证书 | 需 root | 安装 CA 到系统区；否则仅用户区，用户手动信任 |

---

## 4. 当前进度（本仓库状态）

- ✅ Go 内核：MITM 抓包、改包、Token 提取、AI 中转站、DeepSeek 桥、Agent 循环、多协议解析、**省 token 会话压缩**（`session.go`）
  —— 已 `go build` / `go test` / `GOOS=android GOARCH=arm64` 全绿。
- ✅ gomobile 绑定：AIStart / AISetProvider / AIAnalyze / AIAgentRun / AIBrowserPending / AIBrowserComplete / **AiChatSession / AiAgentRunSession / AiSessionNew|List|Clear / AiSetCompact|CompactRatio** 等。
- ✅ 安卓壳：浏览器（内置 `home.html`） + 抓包面板 + Token 面板 + **AI 聊天/会话面板（省 token 开关 + ratio + 会话卡片）** + **一键证书安装**（`android.credentials.INSTALL` + FileProvider 兜底 + Network Security Config 信任用户 CA）+ 抓包横幅，均已写。
- 🟡 CI 出 APK（GitHub Actions `Build APK` 已配）、部分真机验证待做（沙箱无安卓设备）。
- 🔜 下一里程碑：**真·窗口化 WM**（浏览器/聊天独立悬浮窗 + dock + 窗口总览 + 浏览器内 AI 悬浮窗）、**UI 可定制**（`ui.json` + HTML/CSS 双格式，可读可改）。

---

## 5. 免费白嫖链路（用户视角）

1. 打开 yami-UA，浏览器登录 chat.deepseek.com（拿到免费额度）。
2. 点「DeepSeek 白嫖」→ 自动导出网页 cookie 喂给中转站。
3. 正常上网，抓包面板实时显示请求；点「AI 分析」让免费 DeepSeek 帮你看风险/提取 token。
4. 切到「AI」面板下指令："帮我打开 xx 并登录，把 token 复制给我"——Agent 在 WebView 里自己操作，结果回传。
5. 全部流量经 MITM + 可选上游代理，安全可审计。
