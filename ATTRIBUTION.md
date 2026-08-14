# 开源归属 / Attribution

ZedScope 在设计与实现上参考了以下开源项目，并在此致谢。

## 参考项目（架构 / 思路）

| 项目 | 地址 | 参考点 |
|------|------|--------|
| **ProxyPin** | https://github.com/wanghongenpin/proxypin | MITM 抓包代理的整体架构思路（本地代理 + HTTPS 中间人 + 证书管理 + 请求重写）。ProxyPin 自身基于 Flutter/Dart，yami-UA 未复制其代码，仅借鉴架构。 |
| **youtoo（嗅觉浏览器）** | https://github.com/qiusunshine/youtoo | 「浏览器为主 + 网络日志」的产品形态参考（其核心仅开放部分模块）。 |
| **sukisu-ui（KernelSU WebUI）** | 风格参考，请核对上游确切仓库地址（如 KernelSU-Next / suki-up 组织下的 sukisu-ui） | 暗色 + 绿色强调的 UI 视觉风格参考。 |
| **cc-switch** | https://github.com/farion1231/cc-switch | 内置 AI 中转站的架构参考：本地代理 + 多 Provider + 协议转换（Anthropic↔OpenAI）+ 自动故障转移 + OpenAI 兼容。yami-UA 用 Go 标准库重写了轻量内核，未复制其 Rust/React 代码。 |
| **deepseek-pp** | https://github.com/zhu1090093659/deepseek-pp | 「DeepSeek 网页白嫖桥」思路：复用已登录的 DeepSeek 网页会话（cookie）做免费 Agent，而非逆向私有端点。yami-UA 在 WebView 内复用登录态驱动对话。 |
| **Coomi-Android** | https://github.com/TensorHub-ORG/Coomi-Android | 聊天式 Agent 工作台的形态参考：本地优先引擎 + 工具调用循环驱动浏览器/设备，像电脑端 Codex 一样完成任务。 |
| **v2ray / xray-core（多协议代理）** | https://github.com/v2fly/v2ray-core · https://github.com/XTLS/Xray-core | 多协议代理（vmess/vless/trojan/ss/socks5）能力参考。yami-UA 负责把分享链接解析为 xray 配置并拉起用户自带的核，不捆绑二进制。 |
| **DeepSeek-Reasonix** | https://github.com/esengine/DeepSeek-Reasonix | **省 token 模式**的上下文压缩思路：稳定前缀（prefix-cache）+ 一个 rolling summary + 近期尾巴（tail），按 `compact_ratio` 触发。yami-UA 在 `go/ai/session.go` 用纯 Go 实现了等价的内容驱动压缩，未复制其代码。 |

> 说明：sukisu-ui 的确切上游仓库地址以官方发布为准，使用前请自行核对并遵守其许可。

## 直接使用的开源技术 / 库

| 技术 / 库 | 用途 | 许可 |
|-----------|------|------|
| [Go 标准库](https://go.dev/)（`net/http`、`crypto/tls`、`crypto/x509`、`bufio` 等） | MITM 代理内核，零外部依赖 | BSD-3 |
| [gomobile / gobind](https://github.com/golang/mobile) | 将 Go 内核编为 Android AAR | BSD-3 |
| [AndroidX WebView](https://developer.android.com/reference/android/webkit/package-summary) | 浏览器内核承载 | Apache-2.0 |
| [AndroidX Material / Material Components](https://github.com/material-components/material-components-android) | UI 组件 | Apache-2.0 |
| [AndroidX RecyclerView / SwipeRefreshLayout](https://developer.android.com/jetpack/androidx) | 列表与下拉刷新 | Apache-2.0 |
| [Gson](https://github.com/google/gson) | JSON 解析 | Apache-2.0 |
| [ProxyController (Android WebView)](https://developer.android.com/reference/android/webkit/ProxyController) | 将 WebView 流量导向本地代理 | (平台 API) |

所有第三方组件均按其各自的开源许可使用。本项目自身代码以 **MIT** 许可发布（见 `LICENSE`）。
