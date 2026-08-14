# 架构说明（yami-UA）

## 1. 整体数据流

```
用户操作 WebView
   │  (所有请求被导向 127.0.0.1:8899)
   ▼
Go MITM Proxy  ── CONNECT 隧道 ──► TLS 中间人（用内存 CA 给每个 host 签发叶证书）
   │                                 │
   │  解密后的请求 ──► 应用「改包规则」──► 转发到真实服务器
   │                                 │
   │  响应返回 ──► 捕获（请求头/体、响应头/体）──► Token 提取 ──► 环形缓冲 Store
   │                                 │
   └── 同进程 Go 运行时 ◀── Kotlin 经 JNI 调用 yami.Yami.* ──┘
```

- 代理与 API 都只绑定 `127.0.0.1`，抓到的数据**不出本机**。
- Go 内核通过 `gomobile bind` 编成 `yami.aar`，作为 Android 依赖被直接调用，无需额外进程间通信。

## 2. Go 内核模块（`go/`）

| 包 | 职责 |
|----|------|
| `ca` | 内存生成根 CA，按 host 签发叶证书（`LeafFor` 带缓存）。私钥不落盘。 |
| `proxy` | MITM 代理主体：`handleConnect`（CONNECT 隧道 + TLS 终止）、`handleHTTP`（明文代理）、`forward`（上游转发 + 捕获 + 改包）、`token.go`（凭据提取）、`modify.go`（改包规则）。 |
| `api` | 本地 JSON API（`/api/captures`、`/api/tokens`、`/api/ca`、`/api/clear`、`/api/mods`），供桌面自测。 |
| `yami` | gomobile 导出层，所有函数入参/出参均为 `string`（JSON），符合 gomobile 类型限制。 |
| `main` | 桌面独立入口，便于本地调试。 |

### 关键实现点
- **CONNECT 中间人**：劫持客户端连接 → 回 `200 Connection Established` → 用 `tls.Server` + `GetCertificate` 按 SNI 动态签叶证书 → 读取解密后的内层请求 → 转发 → 回写响应。
- **上游校验**：转发到真实服务器时使用 `InsecureSkipVerify:false`，只对我们自己签的「客户端侧」证书做 MITM，对真实服务器仍做正常 TLS 校验。
- **Token 提取**（`ExtractTokens`）：
  - 请求/响应头：`Authorization`、`Set-Cookie` 中的会话型 cookie、`*token*`/`*auth*` 类头。
  - Body：JWT 正则、`"access_token":"..."` 类 JSON 字段。
  - 去重由 API 层按 `key+value` 完成。
- **登录识别**（`detectLogin`）：URL 命中 `/login|/auth|/token|...`，或响应 `Set-Cookie` 含会话，或 body 含 token 字段。

## 3. 改包规则（JSON 数组）

```json
[
  {
    "match_url": "https://api\\.example\\.com/login",
    "header_add": { "X-Debug": "1" },
    "body_regex": "\"role\":\"user\"",
    "body_replace": "\"role\":\"admin\""
  }
]
```
- `match_url`：为空则匹配全部；命中后才注入头 / 替换体。
- 经 `yami.Yami.setMods(json)` 下发。

## 4. 安卓外壳（`android/`）

- `MainActivity`：WebView 浏览器（主界面）+ 底部导航（浏览器 / 抓包 / Token / AI / 设置）。
- `ProxyController`（API 29+）把 WebView 流量导向本地代理。
- `YamiCore`：对 `yami.Yami` 的 Kotlin 封装 + Gson 解析。
- 设置页：启动引擎、纯净捕获开关（默认开，过滤 AI/中转/代理池）、全局抓包(VpnService)开关、导出并引导安装 CA、清空记录。
- Token 页：「一键复制全部 Token」把 `key = value` 逐行写入剪贴板。
- `VpnCaptureService` + `VpnForwarder`：v2 Demo 的全局抓包（TUN 解析 + DNS 转发 + NAT 拼接），默认关。

## 5. 已知限制 / 边界
- **HTTP/2**：内核对上游走真实 `h2`（ALPN `h2`），内层隧道由 net/http 协商 H2/H1.1，**不再降级**（v2 已修复）。
- **全局抓包**：默认只抓本 App 的 WebView 流量（更干净、更省电）；可选「全局抓包」(`VpnService`) Demo 劫持全部 App 流量，默认关，需在真机验证（沙箱无设备）。
- **大包捕获**：响应体流式捕获，内存 8 MiB + 落盘 256 MiB，**无 2 MiB 上限**，转发路径零截断（v2 已修复）。
- 代理拦截依赖 `ProxyController`，需 Android 10+；旧版本或跨 App 抓包用「全局抓包」方案。
- UI 采用 Material You 动态取色（sukisu 风格），Android 12+ 随壁纸取色，旧版本回退紫。
