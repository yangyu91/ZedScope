# ZedScope

> 内置抓包调试的轻量安卓 **AI 浏览器**。主用途是**浏览器**，顺手把流量抓下来、能改包、能一键复制 Token；并内置一个**免费的 AI 中转站**，让 AI 直接分析抓包、甚至自己操作浏览器——全程流量可走代理，安全可审计。

- 内核：**Go 标准库为主**实现的 MITM 代理 + AI 中转站 + 多协议代理（HTTP/HTTPS 拦截、改包、Token 提取、OpenAI 兼容中继、DeepSeek 网页白嫖桥、Agent 工具循环、vmess/vless/trojan/ss 解析），可交叉编译到 `android/arm64`。**唯一外部依赖是 `golang.org/x/crypto/ssh`**（SSH 会话类型用，见下），其余零外部依赖。
- 外壳：**Kotlin + Android WebView**，UI 采用 **Material You 动态取色（Material 3 Dynamic Colors）**，中性暗底 + 大圆角的 sukisu 设计语言（Android 12+ 取色跟随壁纸，旧版本回退紫）。
- 集成方式：Go 内核经 `gomobile bind` 编成 `yami.aar`，由安卓端直接调用。

> ⚠️ **合法使用声明**：本工具仅用于调试**你本人有权限**的流量（你自己的设备、自己的账号、自己拥有的服务）。抓取他人流量、未授权入侵均属违法。证书（CA）仅安装在你自己设备上，流量不外传（API 仅绑定 `127.0.0.1`）。

---

## 30 秒上手（两步拿到 APK）

你不需要本机装任何安卓工具——仓库推上去后，GitHub 云端会自动把好东西编译成 APK。

```bash
cd ZedScope
export GH_TOKEN=ghp_你的令牌        # 需要 repo 权限
./push.sh                            # 建仓库 ZedScope + 推源码（自动触发构建）
```

跑完打开 `https://github.com/<你的用户名>/yami-UA/actions` → 等 `Build APK` 跑完 →
下载 **ZedScope-apk** 产物 → 安装即可。

> 想在本机编译？见下方「本机构建」。`push.sh` 只把仓库推到 GitHub，令牌仅从环境变量读取、绝不写入仓库。

---

## 功能

| 功能 | 说明 |
|------|------|
| 内置浏览器 | WebView 承载，地址栏直达，默认走本地代理 |
| 一键抓包 | 所有经 WebView 的请求/响应被 MITM 拦截并入库 |
| **默认干净捕获** | 默认过滤掉本机控制面 + AI/中转厂商（OpenAI/Anthropic/DeepSeek/Google/Mistral/v2ray/xray 等），列表只留你真正浏览的流量；设置里可一键关掉 |
| **HTTP/2 双模** | 上游走真实 `h2`（ALPN `h2`），内层隧道由 net/http 自行协商 H2/H1.1，**不再降级**现代站点体验 |
| **流式大包捕获** | 响应体经 `io.TeeReader` 边转发边捕获，内存 8 MiB + 落盘 256 MiB，**无 2 MiB 上限**，转发路径零截断 |
| 改包 | 按 URL 正则注入请求头、替换请求体（`ModifyRule`） |
| 一键复制请求 | 点抓包条目即复制请求详情 |
| 自动复制 Token | 检测到登录（路径/Set-Cookie/JSON token）自动标记，Token 面板可一键复制全部 |
| Token 提取 | 覆盖 `Authorization`、JWT、`Set-Cookie` 会话、`access_token` 等 JSON 字段 |
| **内置 AI 中转站** | `127.0.0.1:8910/v1/chat/completions`，OpenAI 兼容，多 Provider + 故障转移，**全部流量可走代理** |
| **DeepSeek 网页白嫖** | 复用 WebView 里已登录的 chat.deepseek.com 会话（cookie），零 API key 免费对话 |
| **AI 抓包分析** | 一键把某条请求喂给 AI，让其识别 token / 风险 / 给可复制 curl |
| **无权限 AI 操作浏览器** | AI 通过 WebView JS Bridge 自己 navigate/click/type/extract，**不申请任何系统权限** |
| **多协议代理** | 解析 vmess/vless/trojan/ss 分享链接 → xray 配置；浏览全走上游代理 |
| **全局抓包（Demo）** | 可选开启 `VpnService`，劫持**全部 App** 流量（默认关，默认仍是只抓浏览器里的） |
| **内置起始页** | 打开 App 即 `home.html`（sukisu 暗色书签页），`yami://install-ca` / `yami://capture` / `yami://settings` 统一入口，告别「只有一个示例网站、点啥都没反应」 |
| **一键证书安装** | 点一下直接用 `android.credentials.INSTALL` 把根证书装为**用户 CA**；并在 `networkSecurityConfig` 里**信任用户 CA**（Android 7+ WebView 默认拒绝用户 CA，正是旧版 HTTPS 打不开 / 抓包空的根因） |
| **抓包横幅** | 没装证书时点网页底部横幅即装；装完自动隐藏，所见即所得 |
| **省 token 模式** | 借鉴 Reasonix：会话上下文按 `compact_ratio`（默认 0.85）压缩为「稳定前缀 + 滚动摘要 + 近期尾巴」，长会话不再爆 token；真实密钥 Provider 默认开，免费桥默认关 |
| **抓包规则引擎** | 借鉴 ProxyPin：域名白/黑名单过滤、请求拦截（按 URL 阻断）、请求映射（本地响应）、AES 解密配置（best-effort 展示明文）；一键 JSON 下发 |
| **HAR 导出 / 搜索** | 全部抓包导出为标准 HAR JSON（可分享/回放）；按关键字 / 状态码 / 响应类型搜索抓包 |
| **多 Provider 健康探测 + 故障转移** | 借鉴 cc-switch：Provider 后台健康探测、失败自动切到下一个健康节点（sticky），多 Provider 不脑裂 |
| **DeepSeek 网页白嫖增强** | 借鉴 deepseek-pp：加固 cookie 注入、SSE 流式 + 思考链分轨、多轮会话保持、登录态失效检测 |
| **Agent 任务编排** | 借鉴 Coomi-Android：工具调用循环 + 离线任务规划（计划→执行→校验）+ 动作单一来源，未知动作安全兜底不 panic |
| **会话导出 / 导入** | 单会话导出为 JSON（备份 / 跨端迁移），可再导入；落盘 JSONL 持久化 |
| **多协议代理增强** | 借鉴 v2ray/xray-core：vmess/vless/trojan/ss/socks5/socks5 分享链接解析为 xray outbound（独立 `proto` 子包，全单测） |
| **SSH 会话类型** | 新增第四种会话：在 AI 面板里填 `host:port` / 用户名 / 密码或私钥，一键建连；命令走底部输入框、结果回写 AI 输出区。后端 `golang.org/x/crypto/ssh` 实现（密码 / 私钥两种认证、`InsecureIgnoreHostKey`、10s 超时、stdout+stderr 合并、非零退出也返回输出），`go/ai/ssh.go` 全单测（4 用例绿） |

---

## 架构

```
 Android App (Kotlin)
   ├─ WebView  ──(ProxyController, 127.0.0.1:8899)──┐
   │   │  AI 指令(JSON) ──┐                          ▼
   │   │                  ▼                  Go MITM Proxy (yami.aar)
   │   │           AiBridge 循环              ├─ CA (内存生成, 每 host 签叶证书)
   │   │              │                       ├─ 捕获 Store (环形缓冲)
   │   └─ YamiCore ───┼──► yami.Yami ◀── 同进程 Go 运行时
   │                  │        ├─ Proxy (抓包/改包/Token/上游代理)
   │                  │        │    ├─ HTTP/2 双模 (上游 h2 + 内层协商)
   │                  │        │    ├─ 流式捕获 (内存 8MiB / 落盘 256MiB, 无上限)
   │                  │        │    └─ 默认纯净过滤 (localhost/AI/中转)
   │                  │        └─ AI Relay (OpenAI兼容 / 多Provider / 故障转移)
   │                  │             ├─ DeepSeek 网页白嫖桥 (复用 cookie)
   │                  │             ├─ Agent 循环 (navigate/click/type/extract/analyze)
   │                  │             └─ SSH 会话 (golang.org/x/crypto/ssh: 密码/私钥/10s超时/合并输出)
   │                  │                  └─ 安卓端 AI 面板建连 + 命令回写输出区
   │                  └──────────────┘ 命令结果回传
   └─ (可选) VpnService ── 全部 App 流量 → local MITM (Demo, 默认关)
```

**免费白嫖闭环**：浏览器登录 DeepSeek 网页 → 导出 cookie 给中转站 → 中转站把抓包喂给免费 DeepSeek 做分析/自动操作 → 全程流量经 MITM + 可选上游代理。

详见 [docs/AI-ARCHITECTURE.md](docs/AI-ARCHITECTURE.md)、[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) 与 [ATTRIBUTION.md](ATTRIBUTION.md)。

---

## 省 token 模式（上下文压缩）

> 设计直接借鉴 [esengine/DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) 的
> **content-driven context maintenance**：稳定前缀 + 一个 rolling summary + 近期尾部，
> 配合 DeepSeek 384K 输出上限，让长会话不爆 token、不丢关键上下文。

**为什么需要**：AI 抓包分析 / 浏览器自动操作时，一轮轮对话很快塞满上下文窗口，
直接把全部历史发给模型既贵又慢。省 token 模式在**本地**对会话做内容驱动的压缩。

**怎么压**（`go/ai/session.go` 的 `SessionStore.Compact`）：

| 组成 | 来源 | 作用 |
|------|------|------|
| 稳定前缀 (prefix) | `system` + **首条 user** | 固定不动，命中模型 **prefix-cache**，省钱省延迟 |
| 滚动摘要 (summary) | 中间消息经活跃 Provider 摘要 | 压缩历史，失败自动降级为「每条前 200 字符」抽取式兜底 |
| 近期尾巴 (tail) | 末 `6` 条原文 | 保留最近上下文，原样保留、不改写 |

- **触发时机**：`estimateTokens(会话) ≥ budget × compact_ratio`。`budget=60000`、`ratio=0.85`（字符数 ≈ token 数 / 4 粗估）。
- **默认策略**（`ApplyDefaultCompact`，切换 Provider 时自动调用）：
  - 活跃 Provider 是**真实密钥型**（`openai` / `anthropic` 且 `api_key≠""`）→ 自动开压缩；
  - `deepseek-web` **免费桥** → 默认关（免费、不必省）。
- **会话持久化**：`SessionStore` + `Session` 用 JSONL + SQLite 投影稳定前缀，命中缓存。
- **零 panic 兜底**：摘要器（模型）失败时不崩溃，落到抽取式摘要，保证总能继续对话。

**接口**（HTTP 与 Kotlin 均已接线）：

```bash
# 开关 / 调比例
curl -X POST http://127.0.0.1:8910/ai/compact        -d '{"on":true}'
curl -X POST http://127.0.0.1:8910/ai/compact_ratio  -d '{"ratio":0.7}'
# 会话管理
curl -X POST http://127.0.0.1:8910/ai/session/new
curl          http://127.0.0.1:8910/ai/session/list
curl -X POST http://127.0.0.1:8910/ai/session/clear -d '{"id":"<session_id>"}'
```

安卓端：AI 面板新增「省 token」开关 + `compact_ratio` 输入框 + 会话卡片（新建会话 / 显示当前 ID）；
填了真实密钥会自动开压缩并 Toast 提示。Agent 自动操作浏览器时也走同一套会话压缩。

> Go 侧 `session_test.go` 已覆盖：token 粗估、压缩后条数下降（42→9，prefix+summary+tail 校验）、
> 尾巴原样保留、摘要失败降级不崩、关闭时 no-op，共 6 用例全绿。

---

## 构建

### 一键出 APK（推荐，无需本机装 SDK）
把仓库推到 GitHub 后，仓库的 **Actions → Build APK** 会自动：装 SDK/NDK → `gomobile bind` 出 `yami.aar` → `assembleRelease`（用调试签名，**产物直接可安装**）→ 上传 APK 产物。下载即可安装。

### 本机构建
前置：Go 1.22+、JDK 17、Android SDK + NDK r25、Gradle 8.9、`gomobile`。

```bash
./build_android.sh                 # 默认出 release (已签名, 可直接 adb install)
BUILD_TYPE=debug ./build_android.sh  # 或出 debug 包
# 产物: android/app/build/outputs/apk/<type>/app-<type>.apk
```

- **签名**：release 默认用 `~/.android/debug.keystore` 自动签名；也可设 `KEYSTORE_FILE/KEYSTORE_PASSWORD/KEY_ALIAS/KEY_PASSWORD` 用自有证书。无论哪种，产物都是可安装的 APK。
- **受限网络镜像**：若 `google()`/`mavenCentral()` 不可达，先 `export YAMI_MAVEN_MIRROR=1` 再跑脚本，改用腾讯 maven 镜像。
- **首次运行**会自动 `go install gomobile/gobind` 并 `gomobile init`。

### 桌面自测（不需要安卓）
```bash
cd go && go run .                       # 代理 127.0.0.1:8899, API 127.0.0.1:8900
https_proxy=http://127.0.0.1:8899 curl -k https://example.com
curl http://127.0.0.1:8900/api/captures
```

### AI 中转站自测（OpenAI 兼容）
```bash
# 假设本地有一个 OpenAI 兼容端点（如 Ollama / 任意中继）：
curl -X POST http://127.0.0.1:8910/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"你好"}],"stream":false}'
# 或让 AI 分析刚抓到的请求：
curl -X POST http://127.0.0.1:8910/ai/analyze \
  -H 'content-type: application/json' \
  -d '{"capture_id":"1","prompt":"这请求泄露了 token 吗？给个 curl"}'
```
> Go 内核的全部能力（`go/ai`、`go/proxy` 的多协议解析、Agent 循环、SSH 会话）已通过 `go test` 与
> `GOOS=android GOARCH=arm64` 交叉编译验证。详见 [docs/AI-ARCHITECTURE.md](docs/AI-ARCHITECTURE.md)。

### 构建排错（APK 打不开 / 构建失败怎么办）

下面这些是**真实踩过的坑**，任何一条没满足都会让 `Build APK` 在编译期挂掉、产物装不上（"打都打不开"）。改完文件后对照自查：

| # | 症状 / 坑 | 正确做法 |
|---|-----------|----------|
| 1 | gomobile 导出名全大写（`CAPEM`）→ Java 端变成 `capem`，Kotlin 调 `Yami.caPEM()` 直接编译错 | 导出函数用**驼峰** `CaPEM` → Java `caPEM`。**不要**改回全大写 |
| 2 | `YamiCore.setBodyDir` 缺失，MainActivity 调用无定义 | `YamiCore.kt` 必须有 `fun setBodyDir(dir): Boolean` 包装 `Yami.setBodyDir` |
| 3 | `gomobile` 不在 `go.mod` 依赖图 → `gomobile bind` 报找不到 | workflow 里必须 `go get golang.org/x/mobile@latest`（**不能 `go mod tidy`**，会把它清掉）|
| 4 | `mkdir -p ../android/app/libs` 漏了 → aar 没地方放 | bind 步骤前先建 `android/app/libs` 目录 |
| 5 | `android.net.ProxyConfig` → AndroidX WebView 无此类，启动崩 | 改用 `androidx.webkit.ProxyConfig`，并 `import androidx.webkit.ProxyConfig` |
| 6 | `Widget.Material3.TextInputEditText` 裸名不存在 → 样式解析失败 | 必须带后缀 `.FilledBox`（即 `Widget.Material3.TextInputEditText.FilledBox`）|
| 7 | `themes.xml` 里自定义 `colorBackground` 属性 → Material3 无此属性 | 删掉 `colorBackground` / `android:colorBackground`；布局改用 `?attr/colorSurface` |
| 8 | VPN `addDisallowedApplication` 不在 `VpnService.Builder` 上 | 必须 `builder.addDisallowedApplication(packageName)` |
| 9 | `establish()` 返回值类型用错 → `onDestroy` 里 `FileDescriptor.close()` 是 API 33+，低版本 `NoSuchMethodError` | 用 `ParcelFileDescriptor` 保存 `established`，`onDestroy` 里 `pfd?.close()`（API 1+ 都有）|
| 10 | `sendTcp(..., ack = true, ...)` 参数名错 → 编译错 | 定义是 `sendTcp(conn, seq, ack, syn, ackF, fin, payload)`，用 `ackF = true`；`fin` 已加默认值 `= false` |
| 11 | `go get golang.org/x/crypto@latest` 把 `go.mod` 升到 go 1.25，CI 工具链不够 → `toolchain not available` | **pin** `golang.org/x/crypto v0.31.0`（兼容 go 1.22~1.25）；CI 的 `setup-go` 已升到 `1.25`（`x/mobile@latest` 需 ≥ 1.25），不再触发工具链下载 |
| 12 | 空仓库首次 push 409 | `push.sh` 已处理；本地用 `git` 初始化提交后再推 |
| 13 | `ProxyController` 用了平台类 `android.webkit.ProxyController`，但其 `setProxyOverride` 只接受 `androidx.webkit.ProxyConfig` → 类型不匹配编译错 | 统一用 `androidx.webkit.ProxyController.getInstance()`（配 `import androidx.webkit.ProxyController`），与 `androidx.webkit.ProxyConfig` 同源 |
| 14 | `RecyclerView.Adapter` 子类里裸写 `getSystemService(CLIPBOARD_SERVICE)` → `Unresolved reference`（常量属于 `Context`） | Adapter 内写全限定 `getSystemService(android.content.Context.CLIPBOARD_SERVICE)`；Activity 方法内（继承 Context）可直接用 `CLIPBOARD_SERVICE` |
| 15 | `sendTcp` 三处调用（`VpnForwarder` 122/138/181）漏传必填 `fin` → `No value passed for parameter 'fin'` | `fin` 加默认值 `= false`，调用处不传即默认 false（显式 `fin = true` 仍有效）|
| 16 | `VpnCaptureService` 在 `onDestroy` 调 `FileDescriptor.close()`（API 33+），Android 8~12 崩溃 | 见 #9：`ParcelFileDescriptor` 保存并 `pfd?.close()` |
| 17 | `builder.allowFamily(...)` 是 API 28+，Android 7/8 启动 VPN 崩 | 加 `Build.VERSION.SDK_INT >= Build.VERSION_CODES.P` 守卫，低版本跳过 |
| 18 | **运行时"点开就闪退"**（构建全绿、资源齐全，但装上即崩）：项目从无 `Application` 类、也从未调 gomobile 的 `Yami.init(ctx)`；首次 `Yami.*` 触发 `.so` 加载与 Go runtime 初始化时，部分 Android 版本因 context 未就绪 **native 崩溃(SIGSEGV/SIGABRT)**，Kotlin `try/catch` 完全拦不住 | 新增 `YamiApplication(:Application)`，在 `onCreate` 最早时机反射调 `Yami.init(applicationContext)`（见 `YamiApplication.kt`）；`AndroidManifest` 注册 `android:name=".YamiApplication"`。删掉该类或把 init 挪出 `Application.onCreate` 必复发 |

> **防回归红线**：`go.mod` 的 `crypto` 必须 pin 在 `v0.31.0`，`gomobile` 必须在依赖图且**不能 `go mod tidy`**（见 #3）。`ProxyController`/`ProxyConfig` 必须**都用 androidx.webkit 同源**；`Adapter` 内 `CLIPBOARD_SERVICE` 必须全限定；`sendTcp` 的 `fin` 参数保持默认值。**`YamiApplication` 必须注册且 `Yami.init(applicationContext)` 必须在 `Application.onCreate` 最早调用**——任何把 `Yami.init` 调用删掉/挪到 Activity、或删掉 `YamiApplication` 的动作都会让"点开就闪退"复发（见 #18）。`ca.NewCA` 已改为返回 error（不再 `panic`），因为 Go 侧 `panic` 会变成 native `SIGABRT` 且 Kotlin 无法捕获。

---

## 使用

1. 打开 App → 浏览器直达内置 `home.html`（书签页）。抓包引擎默认已自动启动。
2. **装证书（关键一步）**：首页点「安装证书」、或浏览时点底部横幅、或设置里「安装抓包证书 (CA)」→ 系统弹窗一键装为**用户 CA**。装完 WebView 才会信任 MITM 叶证书，HTTPS 站点才能打开、流量才能被抓到。
   - 已内置 `networkSecurityConfig` 信任用户 CA（Android 7+ 默认拒绝，旧版正是卡在这里）。
   - 兜底：若一键装失败，会从 `FileProvider` 分享 `.crt` 走系统安装器。
3. 切回浏览器，正常上网。默认就是**干净捕获**：代理池 / AI / 中转流量不会污染列表，需要看全部就关掉「纯净捕获」开关。
4. 底部「抓包」看请求；「Token」看提取到的凭据，点「一键复制全部 Token」。
5. AI 面板：填真实密钥会自动开「省 token 模式」；点「新建会话」开新对话，Agent 自动操作浏览器也走同一套压缩。
   - **SSH 会话**：AI 面板底部「SSH」卡片，填 `host:port`（默认 `127.0.0.1:22`）/ 用户名（默认 `root`）/ 密码或私钥（开「密钥模式」开关即走私钥），点「连接」；底部输入框敲命令、点「执行」，结果回写到 AI 输出区。后端 `golang.org/x/crypto/ssh`，密码/私钥两种认证。
6. （可选）设置里打开「全局抓包」开关 → 授权 VPN → 劫持**全部 App** 流量（Demo 功能，默认关）。
7. 在「设置」里可配置改包规则（JSON 数组，见 `docs/ARCHITECTURE.md`）。

> 代理拦截需 **Android 10+**（依赖 `ProxyController`）。Android 9 及以下或想抓其他 App，用「全局抓包」(VpnService) 方案。

---

## 已知边界（v1）

v1 已修复并默认关闭/移除以下限制，保留说明供对照：

- ~~上游为 HTTP/2 时会被降级到 HTTP/1.1~~ → **已修复**：内核现在对上游走真实 `h2`，内层隧道由 net/http 协商 H2/H1.1，不再降级。
- ~~仅拦截走 WebView 代理的流量~~ → **已增强**：默认仍是只抓浏览器（更干净、更省电）；新增可选「全局抓包」(VpnService) Demo，可劫持全部 App 流量。
- ~~超过 2 MiB 的响应体不进捕获~~ → **已修复**：响应体流式捕获，内存 8 MiB + 落盘 256 MiB，**无 2 MiB 上限**，转发路径零截断。
- ~~打开只有一个示例站、点啥没反应~~ → **已修复**：内置 `home.html` 书签页 + `yami://` 统一入口，浏览器默认可用、可上网。
- ~~证书装了 HTTPS 仍失败 / 抓包空~~ → **已修复**：`android.credentials.INSTALL` 一键装用户 CA + `networkSecurityConfig` 信任用户 CA（Android 7+ 关键遗漏），装完即抓。

仍需注意：

- 「全局抓包」为 **Demo 级实现**，TUN 封包解析 / DNS 转发 / NAT 拼接已在代码中完成，但**需在真机验证**（沙箱无安卓设备），稳定性以真机为准。
- Go 内核以标准库为主；**唯一外部依赖 `golang.org/x/crypto/ssh`**（SSH 会话类型用，已 pin `v0.31.0` 兼容 CI 的 go 1.22），无原生 CGO 依赖。`yami.aar` 通过 `gomobile bind` 生成，已 `GOOS=android GOARCH=arm64` 交叉编译通过；`go/ai` 的省 token、多协议解析、SSH 会话单测全绿。
- 安卓 APK 需在**本机或 CI（GitHub Actions）** 出包（沙箱无 Android SDK）；Go 内核与单测可在沙箱直接跑。

---

## 路线（v2 规划）

| 模块 | 状态 | 说明 |
|------|------|------|
| 浏览器可用化 + 一键证书 | ✅ 已完成 | home.html + `android.credentials.INSTALL` + Network Security Config 信任用户 CA + 抓包横幅 |
| 省 token 模式 | ✅ 已完成 | 借鉴 Reasonix 的上下文压缩（prefix + rolling summary + tail），真实密钥默认开 |
| 抓包规则引擎 / HAR / 搜索 | ✅ 本轮完成 | 借鉴 ProxyPin：域名过滤 + 拦截 + 映射 + AES 解密 + HAR 导出 + 搜索 |
| 多 Provider 健康探测 + 故障转移 | ✅ 本轮完成 | 借鉴 cc-switch：健康探测 + sticky 故障转移 |
| DeepSeek 白嫖桥增强 | ✅ 本轮完成 | 借鉴 deepseek-pp：SSE 流式 + 思考链 + 多轮 + 登录态检测 |
| Agent 任务编排 | ✅ 本轮完成 | 借鉴 Coomi-Android：规划循环 + 动作单一来源 |
| 会话导出/导入 + 持久化 | ✅ 本轮完成 | JSON 往返 + JSONL 落盘 |
| 多协议代理增强 | ✅ 本轮完成 | 借鉴 v2ray/xray-core：独立 `proto` 子包全单测 |
| SSH 会话类型 | ✅ 本轮完成 | `golang.org/x/crypto/ssh`：密码/私钥认证、AI 面板建连 + 命令回写；首个非标准库依赖 |
| **真·窗口化 WM** | 🔜 下一块 | 浏览器 / 聊天都是独立**悬浮窗口**，dock + 窗口总览（一键预览所有窗口）；AI 在浏览器内做独立**悬浮窗**，点一下即发指令开干，免去来回切会话 |
| **UI 可定制（落地中）** | 🟡 雏形已出 | 顶栏 / 主窗口 / 浏览器 / 抓包等 UI 可由 `ui.json` 配置（JSON + HTML/CSS 双格式，可读可改）；原生侧 `yamiUi` 读写桥待 MainActivity 接入 |

> **本轮作战方式**：以「公司化并行多 Agent」推进——总办统筹，7 个部门 Agent 各自认领一个参考仓库
> （① 抓包内核部/ProxyPin、② AI 中转站部/cc-switch、③ 白嫖桥部/deepseek-pp、④ Agent 自动化部/Coomi-Android、
> ⑤ 省 token 部/Reasonix、⑥ UI 设计部/sukisu-ui+youtoo、⑦ 多协议代理部/v2ray-xray），并行补差距、出模块，
> 最后由总办在 `yami.go` / `api.go` / `relay.go Handler()` / `YamiCore.kt` / `MainActivity.kt` 统一收口集成。
> Go 侧收口后 `go build` / `go vet` / `go test` 全绿（107 PASS）。

## 许可与归属
- 代码许可：**MIT**（见 [LICENSE](LICENSE)）。
- 参考的开源项目与库见 [ATTRIBUTION.md](ATTRIBUTION.md)。
