/* =====================================================================
   ZedScope · UI 定制运行时（planned feature 的落地雏形）
   ---------------------------------------------------------------------
   职责：
     1. 解析配置，优先级 localStorage['zedscope.ui.config'] > assets/ui.json
        > window.ZEDSCOPE_UI_DEFAULTS（ui-defaults.js）
     2. 把 theme.* 写成 ui-tokens.css 的 CSS 自定义属性（accent / 圆角 /
        密度 / 动效），让起始页与配置页实时换肤
     3. 提供保存、导出、重置、区块可见性等公共能力给 home.html 与
        ui-config.html 复用
   原生侧接入点（尚未实现，见 ui.schema.json 的 x-scope=native）：
     若 MainActivity 之后注入 window.yamiUi = { read(), write(json) }
     的 @JavascriptInterface，本文件会自动优先使用它，从而把配置真正落盘到
     ui.json；探测失败则退回 localStorage，不影响现有功能。
   ===================================================================== */
(function (global) {
  "use strict";

  var KEY = "zedscope.ui.config";
  var CONFIG_URL = "ui.json";

  /* ---------------- 工具 ---------------- */

  function isObj(v) {
    return v !== null && typeof v === "object" && !Array.isArray(v);
  }

  /** 深合并：用 patch 覆盖 base，数组整体替换（书签/导航顺序语义上是整体） */
  function merge(base, patch) {
    if (!isObj(base)) return clone(patch);
    var out = clone(base);
    if (!isObj(patch)) return out;
    Object.keys(patch).forEach(function (k) {
      var pv = patch[k];
      out[k] = isObj(pv) && isObj(out[k]) ? merge(out[k], pv) : clone(pv);
    });
    return out;
  }

  function clone(v) {
    return isObj(v) || Array.isArray(v) ? JSON.parse(JSON.stringify(v)) : v;
  }

  function hex2rgb(hex) {
    var h = String(hex || "").replace("#", "").trim();
    if (h.length === 3) h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
    if (!/^[0-9a-fA-F]{6}$/.test(h)) return null;
    return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
  }

  function rgb2hex(c) {
    return "#" + c.map(function (n) {
      var s = Math.max(0, Math.min(255, Math.round(n))).toString(16);
      return s.length === 1 ? "0" + s : s;
    }).join("");
  }

  function mix(a, b, t) {
    var x = hex2rgb(a), y = hex2rgb(b);
    if (!x || !y) return a;
    return rgb2hex([0, 1, 2].map(function (i) { return x[i] + (y[i] - x[i]) * t; }));
  }

  function luma(hex) {
    var c = hex2rgb(hex);
    if (!c) return 0;
    return (0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]) / 255;
  }

  function rgba(hex, a) {
    var c = hex2rgb(hex);
    return c ? "rgba(" + c[0] + "," + c[1] + "," + c[2] + "," + a + ")" : hex;
  }

  /* ---------------- 主题应用 ---------------- */

  var DENSITY = { compact: "3px", normal: "4px", comfortable: "5px" };

  /**
   * 把 theme.* 落到 :root 的 CSS 变量上。
   * accent 之外的派生色（accent-soft / on-accent / glow / ripple）自动算出来，
   * 这样用户只挑一个强调色也能保持对比度与暖琥珀观感。
   */
  function applyTheme(cfg) {
    var t = (cfg && cfg.theme) || {};
    var root = document.documentElement;
    var set = function (k, v) { if (v) root.style.setProperty(k, v); };

    var accent = /^#[0-9a-fA-F]{6}$/.test(t.accent || "") ? t.accent : "#D9A45B";
    var bg = /^#[0-9a-fA-F]{6}$/.test(t.background || "") ? t.background : "#171513";
    var surface = /^#[0-9a-fA-F]{6}$/.test(t.surface || "") ? t.surface : "#1E1C19";
    var accentDim = /^#[0-9a-fA-F]{6}$/.test(t.accentDim || "") ? t.accentDim : mix(accent, "#000000", 0.28);

    set("--accent", accent);
    set("--accent-dim", accentDim);
    set("--accent-soft", mix(bg, accent, 0.14));
    set("--on-accent", luma(accent) > 0.55 ? mix(accent, "#000000", 0.86) : "#FFFFFF");
    set("--on-accent-soft", mix(accent, "#FFFFFF", 0.45));
    set("--accent-glow", rgba(accent, 0));   /* 刻意无发光（anti-slop） */
    set("--accent-ripple", rgba(accent, 0.2));

    set("--bg", bg);
    set("--surface", surface);
    set("--surface-2", mix(surface, "#FFFFFF", 0.04));
    set("--surface-3", mix(surface, "#FFFFFF", 0.09));
    set("--surface-4", mix(surface, "#FFFFFF", 0.14));
    set("--glass", rgba(surface, 0.92));
    set("--line", mix(surface, "#FFFFFF", 0.08));

    var r = typeof t.radius === "number" ? Math.max(0, Math.min(32, t.radius)) : 22;
    set("--r-lg", r + "px");
    set("--r-md", Math.round(r * 0.72) + "px");
    set("--r-sm", Math.round(r * 0.55) + "px");
    set("--r-xl", Math.round(r * 1.27) + "px");

    set("--s-unit", DENSITY[t.density] || DENSITY.normal);

    var motion = t.motion !== false;
    set("--dur", motion ? ".2s" : "0s");
    set("--dur-fast", motion ? ".12s" : "0s");
    root.setAttribute("data-motion", motion ? "on" : "off");
  }

  /* ---------------- 读写 ---------------- */

  function defaults() {
    return clone(global.ZEDSCOPE_UI_DEFAULTS || {});
  }

  function readLocal() {
    try {
      var raw = global.localStorage && global.localStorage.getItem(KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (e) {
      return null;
    }
  }

  /** 原生桥（尚未实现，探测式使用） */
  function bridge() {
    var b = global.yamiUi;
    return b && typeof b.read === "function" ? b : null;
  }

  function fetchAsset() {
    return new Promise(function (resolve) {
      if (typeof fetch !== "function") return resolve(null);
      fetch(CONFIG_URL, { cache: "no-store" })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(resolve)
        .catch(function () { resolve(null); }); // file:// 下被 CORS 拦截属预期
    });
  }

  /**
   * 解析最终配置。
   * @returns {Promise<{config:Object, source:string}>} source: local|asset|bridge|builtin
   */
  function load() {
    var base = defaults();
    var b = bridge();
    if (b) {
      try {
        var fromNative = JSON.parse(b.read());
        if (fromNative) return Promise.resolve({ config: merge(base, fromNative), source: "bridge" });
      } catch (e) { /* 落到下一档 */ }
    }
    var local = readLocal();
    if (local) return Promise.resolve({ config: merge(base, local), source: "local" });
    return fetchAsset().then(function (asset) {
      return asset
        ? { config: merge(base, asset), source: "asset" }
        : { config: base, source: "builtin" };
    });
  }

  /** 保存：优先原生落盘，其次 localStorage。返回实际去处。 */
  function save(cfg) {
    var out = clone(cfg);
    if (!out.meta) out.meta = {};
    out.meta.updatedAt = new Date().toISOString();
    var json = JSON.stringify(out, null, 2);
    var b = global.yamiUi;
    if (b && typeof b.write === "function") {
      try { b.write(json); return { ok: true, where: "ui.json" }; } catch (e) { /* fallthrough */ }
    }
    try {
      global.localStorage.setItem(KEY, json);
      return { ok: true, where: "localStorage" };
    } catch (e) {
      return { ok: false, where: "-", error: String(e) };
    }
  }

  function reset() {
    try { global.localStorage.removeItem(KEY); } catch (e) { /* ignore */ }
  }

  function toJson(cfg) {
    return JSON.stringify(cfg, null, 2);
  }

  /* ---------------- 区块可见性 ---------------- */

  /**
   * 按 data-block="home.showSearch" 批量隐藏/显示节点。
   * 供 home.html 使用；原生区块由 Kotlin 侧读同名字段（待接入）。
   */
  function applyBlocks(cfg, scope) {
    var nodes = (scope || document).querySelectorAll("[data-block]");
    Array.prototype.forEach.call(nodes, function (el) {
      var path = el.getAttribute("data-block");
      var v = path.split(".").reduce(function (acc, k) {
        return acc == null ? acc : acc[k];
      }, cfg.blocks || {});
      el.hidden = v === false;
    });
  }

  /* ---------------- 地址栏 / 搜索 ---------------- */

  var URL_LIKE = /^([a-z][a-z0-9+\-.]*:\/\/|localhost|(\d{1,3}\.){3}\d{1,3}|[\w-]+(\.[\w-]+)+)(:\d+)?(\/|$|\?)/i;

  /** 输入既能当网址也能当搜索词：像网址就直达，否则套 search.template */
  function resolveInput(text, cfg) {
    var q = String(text || "").trim();
    if (!q) return "";
    if (/^[a-z][a-z0-9+\-.]*:\/\//i.test(q)) return q;
    if (URL_LIKE.test(q) && !/\s/.test(q)) return "https://" + q;
    var tpl = (cfg && cfg.search && cfg.search.template) || "https://www.bing.com/search?q=%s";
    return tpl.replace("%s", encodeURIComponent(q));
  }

  /* ---------------- 轻量 toast ---------------- */

  var toastTimer = null;
  function toast(msg) {
    var el = document.getElementById("yamiToast");
    if (!el) {
      el = document.createElement("div");
      el.id = "yamiToast";
      el.className = "toast";
      document.body.appendChild(el);
    }
    el.textContent = msg;
    // 强制回流后再加 class，保证过渡生效
    void el.offsetWidth;
    el.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { el.classList.remove("show"); }, 1800);
  }

  global.YamiUI = {
    KEY: KEY,
    load: load,
    save: save,
    reset: reset,
    merge: merge,
    clone: clone,
    defaults: defaults,
    applyTheme: applyTheme,
    applyBlocks: applyBlocks,
    resolveInput: resolveInput,
    toJson: toJson,
    toast: toast,
    hasBridge: function () { return !!bridge(); }
  };
})(window);
