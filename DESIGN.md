# ZedScope — Design System ("Cyber Glass")

面向「内置抓包调试的轻量 AI 浏览器」这一技术工具定位重构的视觉系统。
灵感来自 [ret2shell](https://github.com/ret2shell/ret2shell) 的赛博霓虹玻璃语言，
受 [impeccable](https://github.com/pbakaus/impeccable)（克制纪律）与
[taste-skill](https://github.com/Leonxlnx/taste-skill)（三拨盘；反 slop）约束。

> 三拨盘取值：**VARIANCE 6 · MOTION 7 · DENSITY 4** —— 有识别度但不花哨，动效灵动但无弹跳。
> 原生层（`res/values/*`）为单一真相源；WebView 起始页已移除，原生与功能完整耦合。

---

## 1. 三条不可妥协的原则

| 原则 | 做法 | 反例（已禁） |
| --- | --- | --- |
| **绝不纯黑纯灰** | 画布冷蓝黑 `#050B0E`（夜）/ 冷白蓝 `#F1F8FB`（日），表面逐层加亮 | 纯黑 `#000`、纯灰、灰字压彩底 |
| **单一克制强调色** | 只用霓虹青（日 `#0089A4` / 夜 `#00C7E0`）一种饱和色 | AI 紫渐变、多彩强调、彩虹状态 |
| **扁平 + 玻璃，无发光** | 零 elevation glow、零渐变填充、零 bounce；玻璃用 72% 半透明 + 发丝线 | `box-shadow` 发光、外发光、弹性回弹 |

---

## 2. 调色板（双主题）

用 Android **DayNight 机制**：同名 token 分置 `values/colors.xml`（日）与
`values-night/colors.xml`（夜），主题 `parent="…DayNight.NoActionBar"` 自动跟随。
**所有布局 / style 零改动**即可双主题。

| token | 日 (cyber-light) | 夜 (cyber-dark) | 角色 |
|---|---|---|---|
| `yami_bg` | `#FFF1F8FB` | `#FF050B0E` | 画布（最深一档） |
| `yami_surface` | `#FFFBFEFF` | `#FF0A1316` | 卡片 / 面板底 |
| `yami_surface_2..4` | 逐级递减 | 逐级递增 | 层次 ramp |
| `yami_surface_glass` | `#B8FBFEFF` (72%) | `#B80A1316` (72%) | **玻璃填充** |
| `yami_accent` | `#FF0089A4` | `#FF00C7E0` | 唯一饱和色（霓虹青） |
| `yami_on_accent_soft` | `#FF00576B` | `#FF9DE1EE` | 青色微光（骨架屏高光） |
| `yami_divider` | `#FFD1DBDF` | `#FF263033` | 发丝线 |
| `yami_warn` / `yami_danger` | 暖色系 | 同色提亮 | 语义（登录点 / 危险） |

色值由 OKLCH（色相 ≈ 222）精确转到 sRGB，保证日/夜同色相、仅明度分层。

**反 slop 红线（impeccable 纪律）**：禁用 AI 紫渐变、禁用发光/外发光、禁用弹跳缓动、
UI 文案禁用 em-dash（界面文案用全角破折号或冒号；em-dash `—` 仅出现在代码注释）。

---

## 3. 玻璃系统（Glass）

原生 XML drawable **无法做 backdrop-blur**，故以「**72% 半透明填充 + 1dp 发丝线**」还原
ret2shell 的玻璃感（其 CSS `backdrop-blur` 是同效果的 Web 版等价物）。

- 浮动 chrome：顶栏 `bg_topbar_soft`、底栏 `bg_bottombar_soft`、输入栏 `bg_inputbar_soft`
  —— 在浏览器里浮于 WebView 之上，透出内容即「玻璃」。
- 胶囊 / 状态：`bg_pill_surface`（抓包计数、VPN/证书状态）。
- 卡片：`bg_card` / `bg_card_lg` / `bg_card_raised` 均为 72% 玻璃，叠在 `yami_bg` 画布上
  形成 frosted 层次（对齐 ret2shell 的 layer/bg 逻辑）。
- 图标键：`bg_icon_btn` 为玻璃圆盘（发丝环），按下时 accent 涟漪绽放。

> **真模糊接口（预留）**：如需 WebView 内容被顶/底栏真模糊，可在 API 31+ 用
> `RenderEffect.createBlur()` 对下方内容做模糊（降级：低版本维持 72% 玻璃）。本轮未启用，
> 避免引入不可编译/不可测风险；玻璃色板已可直接套用。

---

## 4. 动效（Motion）

所有转场用统一时钟，ease-in-out、**无弹跳**。位移用 `%p`（相对父容器，分辨率无关）。

| 动画 | 方向 / 用途 | 时长 |
|---|---|---|
| `slide_fade_in` | 通用进入（上浮 + 渐显） | 300ms |
| `slide_fade_out` | 通用退出（上移 + 渐隐） | 200ms |
| `slide_fade_right_in` | 钻入子页（从右进） | 300ms |
| `slide_fade_left_in` | 返回枢纽 / 浏览器（从左进） | 300ms |
| `slide_fade_left_out` / `right_out` | 与进入镜像的退出，构成「转轮式」切换 | 220ms |
| `fade_in` / `fade_out` | 对话框 / 浮层 | 200 / 180ms |
| 骨架屏扫光 | 105° 对角青色微光（`ui/Skeleton.kt`） | 循环 |

沉浸模式（`hideChrome` / `showChrome`）顶/底栏 `translationY` 180ms 滑入滑出。

---

## 5. 架构（Architecture）

- **双主题**：`AppCompatDelegate.setDefaultNightMode(...)` + `SharedPreferences("zedscope","theme_mode")`。
  `applySavedNightMode()` 在 `onCreate` 最前调用（无闪白），`setupThemeToggle()` 绑定
  设置页「自动 / 浅色 / 深色」分段控件（MaterialButtonToggleGroup）。
- **面板切换**：7 个面板包进重叠的 `panelHost` FrameLayout（`activity_main.xml`）。
  旧实现用垂直 LinearLayout + weight，转场时两面板同时 VISIBLE 会各占 50% 高度错位；
  改为重叠 FrameLayout 后可安全**交叉淡入淡出**。`showPanel(which)` 据
  `fromSub / toSub` 选择方向动画，退出面板 `postDelayed(220)` 后才 `GONE`，
  并用 `v !== panels[currentPanel]` 守卫防止快速切换时误隐藏刚重新进入的面板。
- **骨架屏**：`SkeletonView` 自绘 105° 对角青色微光，`TypingDots` 三颗霓虹青错峰呼吸。
- **资源纪律**：所有 `R.id` / drawable / color / style **名称不变**，仅重写视觉值；
  改动后用 `validate_res.py`（引用完整性）+ `check_wellformed.py`（XML 良构）静态校验。

---

## 6. 反 slop 检查清单（提交前自查）

- [ ] 无系统默认字体栈之外的中文回退缺失
- [ ] 无灰字压彩底、无纯黑纯灰
- [ ] 无渐变填充（玻璃除外：仅 72% 半透明 + 发丝线）、无阴影发光
- [ ] 无卡片套卡片、无 bounce/elastic 动效
- [ ] 无 AI 紫、无多彩强调
- [ ] 触摸目标 ≥ 44dp、标题层级不跳级、垂直节奏一致

---

## 7. 扩展指引

- 新增颜色：在 `values/colors.xml` 与 `values-night/colors.xml` **同步**加同名 token。
- 新增玻璃面：直接 `android:background="@drawable/bg_card"`（或 `*_lg` / `*_raised`）。
- 新增转场：复用 `anim/slide_fade_*`；新面板在 `showPanel` 的 `panels` 数组登记即可。
- 改主题色：只动 `colors.xml` 两套，布局无需碰。
