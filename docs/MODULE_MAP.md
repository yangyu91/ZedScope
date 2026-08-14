# ZedScope · 模块地图与逻辑定位

> 每个模块在动手前都先过两个 skill 的逻辑定位三问：
> **① 这个模块我要怎么做？ ② 它和上级/同级如何过渡？ ③ 连贯性/交互/留白如何处理？**
> 本文件是审计锚点——改某处时先回看这里的"逻辑定位"，保证自洽一体。

---

## 顶层架构（单 Activity + 多面板）

```
MainActivity (god-class 已瘦身)
 ├─ BrowserPanel      浏览器（WebView + 顶栏 + 进度）
 ├─ CapturePanel      抓包（SwipeRefresh + RecyclerView + 骨架屏）
 ├─ TokenPanel        Token（RecyclerView + 骨架屏）
 ├─ AiPanel           AI 对话/办事（气泡 + 流式打字 + TypingDots）
 ├─ SettingsPanel     设置（引擎/证书/全局抓包/VPN/关于）
 └─ ui/               SkeletonView · TypingDots（自绘，零依赖）

YamiCore.kt           Kotlin ↔ gomobile `yami.Yami` 薄封装（唯一原生↔Go 边界）
AiBridge.kt           WebView JS 命令轮询桥
VpnForwarder/VpnCaptureService  全局抓包（VpnService TUN）
YamiApplication.kt    反射 init + boot.log 启动轨迹 + crash handler
ChatAdapter.kt        AI 气泡适配器
CaptureAdapter.kt     抓包卡片适配器（已从 MainActivity 抽出）
TokenAdapter.kt       Token 卡片适配器（已从 MainActivity 抽出）

Go core (yamiua module, 预编译 AAR，不可本地重建)
 └─ yami.Yami         MITM / 抓包 / Token 提取 / AI 中转 / 协议解析
```

---

## 模块逻辑定位

### 1. 抓包面板 `CapturePanel`
- **怎么做**：`SwipeRefreshLayout` 外包 `FrameLayout`；内层 `rvCapture` + 覆盖层 `skeletonCapture`。
- **与上级/同级过渡**：骨架屏与真实列表**同容器**——加载时骨架盖在上面，`alpha 1→0`
  交叉淡出后真实列表浮现，位置不跳变（同级过渡，非独立页）。
- **连贯/留白**：每条卡片用发丝线分隔（`--line`），无阴影；空态用 `empty_capture` 居中提示。
- **审计点**：`refreshCapture()` 经 `loadWithSkeleton()` 走骨架；`CaptureAdapter` 独立文件，点击复制整条请求详情。

### 2. Token 面板 `TokenPanel`
- **怎么做**：与抓包面板**同构**（对齐=可预测），仅换 `rvToken` + `skeletonToken` + `TokenAdapter`。
- **连贯**：两面板共享同一套骨架/卡片/空态语言，用户切换零学习成本。
- **审计点**：`TokenAdapter` 点击复制单值；`tvLoginFlag` 仅在 `is_login` 时显隐。

### 3. AI 面板 `AiPanel`
- **怎么做**：对话式气泡（`ChatAdapter`）+ 流式打字机；四个运行入口
  （分析/AI 运行/Agent/SSH）原先塞"思考中"文字气泡，**改为 `TypingDots` + 后台线程**。
- **交互**：`showTyping()` 显示三点 → 后台完成 `hideTyping()` → 追加空气泡 → `streamInto()` 逐字流入。
  用 `TypingDots` 替代文字气泡，避免"思考中"与真实内容抢视觉层级。
- **留白**：输入栏与对话区之间固定间距，TypingDots 贴底对齐输入栏上方。

### 4. 浏览器 `BrowserPanel`
- **怎么做**：`WebView` + 顶栏（后退/前进/刷新/盾牌）+ 进度条 + 证书引导 banner。
- **与上级过渡**：起始页 `home.html` 通过 `yami://` 深链跳各面板，深链即"过渡契约"，
  使 WebView 起始页与原生面板像同一产品的两面。
- **审计点**：`yami://capture|ai|install-ca|settings` 是起始页↔原生的唯一跳转协议。

### 5. 起始页 `home.html` + 运行时 `ui-boot.js`
- **怎么做**：Claude 极简排版，暖中性 + 单一琥珀强调；`ui-tokens.css` 与原生 colors.xml 同步。
- **连贯**：起始页的 `--accent`/`--r-*`/`--s-*` 来自同一 token 体系，读起来与原生同呼吸。
- **可换肤**：`ui-boot.js` 把 `ui.json` 的 `theme.*` 重写成 CSS 变量；**刻意关闭 `--accent-glow`**
  （`rgba(accent,0)`，反 slop）；默认强调色 `#D9A45B`。

### 6. 设计系统 `ui/ + res/values/* + ui-tokens.css`
- **一致性锁**：颜色/圆角/间距/动效全局锁死，原生与 WebView 三处同步（见 `DESIGN.md`）。
- **骨架屏**：`SkeletonView`（自绘 shimmer，attach/detach 自理生命周期）、`TypingDots`
  均零依赖，不引入任何动画库。

### 7. 原生↔Go 边界 `YamiCore.kt`
- **怎么做**：`YamiCore` 是**唯一**调用 gomobile `yami.Yami` 的地方；所有重活（MITM/抓包/AI）
  在 Go 侧，Kotlin 只做序列化（Gson）+ UI 投递。
- **审计点**：每个方法 `try/catch` 降级为空结果，避免 Go 异常击穿 Kotlin 构建/运行。

---

## 已知边界（改名/构建约束）

- **Go core 冻结**：`yamiua` 模块与 gomobile `yami.Yami` 绑定属**预编译 AAR**，沙箱无法重建。
  Go 源码内的 `yami-UA` 字符串（CA CommonName、HAR 创建者名、过滤正则 `yami-ua`、系统提示词）
  **写死在 AAR 中**，改源码对出包无效；过滤正则尤其不可动（动则改变抓包行为）。
- **包名已改**：`com.yamiua.app` → `com.zedscope.app`（目录/源码/布局/gradle/proguard 全改，`yami.Yami` 不动）。
- **仓库重命名**：GitHub 仓库 `yangyu91/yami-UA` 需手动在 GitHub 改名（受 token 限制未自动完成）。
