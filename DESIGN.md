# ZedScope · 设计系统（Claude 极简 + 暖中性）

本文件是 ZedScope 的**单一设计真相源**。原生层（`res/values/*`）、WebView 层
（`assets/ui-tokens.css`）在设计上严格对齐，使起始页与四个原生面板读起来像**一个产品**。

设计纪律来自两个 skill：
- [`taste-skill`](https://github.com/Leonxlnx/taste-skill) —— 排版/颜色/布局/交互的硬禁令与 100+ 审计清单
- [`impeccable`](https://github.com/pbakaus/impeccable) —— 去 AI slop 的视觉信号清单

---

## 1. 三条不可妥协的原则

| 原则 | 做法 | 反例（已禁） |
| --- | --- | --- |
| **绝不纯黑纯灰** | 画布用暖暗 `#171513`，表面逐层加亮；任何文字/分隔都带暖色调 | 纯黑 `#000`、纯灰 `#808080`、灰字压彩底 |
| **单一克制强调色** | 只用暖琥珀 `#D9A45B` 一种饱和色，其余皆中性 | 紫蓝渐变、多彩强调、彩虹状态 |
| **扁平无发光** | 零 elevation glow、零渐变、零 bounce 弹性动效 | `box-shadow: 0 6px 22px` 发光、卡片套卡片、弹性回弹 |

## 2. 设计 token（三处同步：colors.xml / dimens.xml / ui-tokens.css）

```
画布   bg #171513  surface #1E1C19  surface-2 #262320  surface-3 #2E2B27  surface-4 #36322D
文字   on #ECE9E3  on_dim #A39E94  on_faint #736E64
强调   琥珀 #D9A45B  accent_dim #B9843F  accent_soft #2C2417  on_accent #1C1407  on_accent_soft #E9CFA1
语义   危险 #E08C7C  警告 #E0B25C（均暖色调）
分隔   line #2E2B27（发丝线，非阴影）
圆角   xs6 sm8 md10 lg14 xl20 dp（收敛，无 pill 滥用）
间距   4dp 基准单位，s-xs→s-xxl 节奏一致
动效   仅短促 ease-out（cubic-bezier(.2,.7,.3,1)），无 bounce；支持 prefers-reduced-motion
```

**一致性锁**：颜色、圆角、间距、动效在原生与 WebView 间全局锁死。改一处必须三处同步，
`assets/ui-tokens.css` 头注释即为同步契约。

## 3. 全局骨架屏（加载即精准占位）

所有列表/网络加载都走骨架屏，而非转圈或空白：

- `ui/Skeleton.kt · SkeletonView` —— 自绘 `LinearGradient` shimmer，**零依赖**，
  `onAttachedToWindow` 启动 / `onDetachedFromWindow` 取消，生命周期自管理。
- `ui/Skeleton.kt · TypingDots` —— AI 思考指示，三椭圆点错位 alpha，替代"AI 正在思考…"文字气泡。
- 覆盖层模式：`FrameLayout` 骨架盖在 `RecyclerView` 上，加载完成后 `alpha 1→0` 交叉淡出
  （`loadWithSkeleton()`，280ms 占位 + 180ms 淡出）。

> 逻辑定位：骨架屏是**同级过渡**而非独立页面——它与真实列表共享同一容器，
> 因此从占位到内容的切换是"同一位置淡入淡出"，而非跳变，符合 GitHub 安卓版式的丝滑感。

## 4. 反 slop 检查清单（提交前自查）

- [ ] 无 Inter/Arial 系统默认字体（用系统栈 + 中文回退）
- [ ] 无灰字压彩底、无纯黑纯灰
- [ ] 无渐变填充、无阴影发光
- [ ] 无卡片套卡片
- [ ] 无 bounce/elastic 动效
- [ ] 无侧边 tab 发光边框、无暗色发光
- [ ] 行宽受控、触摸目标 ≥ 44dp、标题层级不跳级
- [ ] 底部 CTA 对齐、垂直节奏一致、光学居中

## 5. 模块逻辑定位（详见 `docs/MODULE_MAP.md`）

每个功能模块在动手前都先回答：
**这个模块我要怎么做？它和上级/同级如何过渡？连贯性/交互/留白如何处理？**
答案写进 `docs/MODULE_MAP.md`，作为后续修改的审计锚点。
