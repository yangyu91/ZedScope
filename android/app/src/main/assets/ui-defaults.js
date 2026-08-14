/* =====================================================================
   ZedScope · built-in fallback copy of assets/ui.json
   ---------------------------------------------------------------------
   Why this file exists: pages loaded from file:///android_asset cannot
   fetch() a sibling JSON file (file:// is an opaque origin unless the app
   turns on allowFileAccessFromFileURLs, which we deliberately do NOT do).
   A <script src> however always works, so ui-boot.js falls back to this
   object when the fetch is blocked.

   KEEP IN SYNC WITH ui.json (same shape, same defaults, same version).
   ui.json stays the canonical, human-editable file for the native side.
   ===================================================================== */
window.ZEDSCOPE_UI_DEFAULTS = {
  version: 1,
  meta: { name: "ZedScope default", author: "UI 设计部", updatedAt: "" },
  theme: {
    mode: "dark",
    monet: false,
    accent: "#D9A45B",
    accentDim: "#B9843F",
    background: "#171513",
    surface: "#1E1C19",
    radius: 14,
    density: "normal",
    motion: true
  },
  blocks: {
    topbar: { visible: true, showBack: true, showForward: true, showReload: true, showShield: true, style: "pill" },
    home: {
      visible: true, showBrand: true, showStatus: true, showSearch: true,
      showQuickActions: true, showCaHint: true, showBookmarks: true, bookmarkColumns: 2
    },
    browser: { visible: true, showProgress: true, showCaptureBanner: true, homeUrl: "file:///android_asset/home.html" },
    capture: {
      visible: true, showStats: true, showMethodChip: true, showSchemeTag: true,
      showLoginFlag: true, cleanCapture: true, pageSize: 200
    },
    token: { visible: true, showSource: true, maskValue: false },
    ai: { visible: true, showCompact: true, showSession: true, showQuickChips: true },
    settings: { visible: true, showVpn: true, showCert: true, showAbout: true },
    bottomNav: { visible: true, labels: true, items: ["browser", "capture", "ai", "token", "settings"] }
  },
  search: { engine: "bing", template: "https://www.bing.com/search?q=%s" },
  bookmarks: [
    { title: "百度", url: "https://www.baidu.com" },
    { title: "Bing", url: "https://www.bing.com" },
    { title: "GitHub", url: "https://github.com" },
    { title: "DeepSeek", url: "https://chat.deepseek.com" }
  ]
};
