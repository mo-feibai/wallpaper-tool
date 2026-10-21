const $ = (id) => document.getElementById(id);

let nextRunAt = null;
let lastDesktopKey = null;
let lastLockKey = null;
let lastError = "";
let linkError = "";

// fetch 包装: 支持超时, 超时抛出带 timedOut 标记的错误。
async function api(path, opts) {
  const o = Object.assign({}, opts || {});
  const timeoutMs = o.timeoutMs;
  delete o.timeoutMs;
  let timer = null;
  if (timeoutMs) {
    const ctrl = new AbortController();
    o.signal = ctrl.signal;
    timer = setTimeout(() => ctrl.abort(), timeoutMs);
  }
  try {
    const resp = await fetch(path, o);
    if (!resp.ok) {
      let msg = await resp.text();
      throw new Error(msg || ("HTTP " + resp.status));
    }
    return await resp.json();
  } catch (e) {
    if (e && e.name === "AbortError") {
      const err = new Error("请求超时");
      err.timedOut = true;
      throw err;
    }
    throw e;
  } finally {
    if (timer) clearTimeout(timer);
  }
}

// 先解码再切换显示: 避免换图时出现空白, 也避免"新信息配旧图片"。
// 失败时保留上一张已成功显示的内容。
async function swapImage(imgEl, url) {
  if (!url) return false;
  const probe = new Image();
  probe.decoding = "async";
  probe.src = url;
  try {
    if (probe.decode) {
      await probe.decode();
    } else {
      await new Promise((resolve, reject) => {
        probe.onload = resolve;
        probe.onerror = () => reject(new Error("图片加载失败"));
      });
    }
  } catch (e) {
    return false;
  }
  imgEl.src = url;
  imgEl.style.display = "";
  return true;
}

// 页面背景就是当前壁纸: 模糊暗化后作为玻璃层的底, 换图时同步更新。
// 先确认图片可用再切换, 避免背景闪一下空白。
function syncBackdrop(url) {
  if (!url) return;
  const probe = new Image();
  probe.onload = () => {
    document.documentElement.style.setProperty("--bg-image", 'url("' + url + '")');
  };
  probe.src = url;
}

function fillForm(c) {
  cfgRev = c.revision || 0;
  setTimeout(syncSelectLabels, 0);
  // 数据源
  $("f-src-desktop").value = (c.sources && c.sources.desktop) || "wallhaven";
  $("f-src-lock").value = (c.sources && c.sources.lock_screen) || "wallhaven";
  // 凭据不回显: 输入框留空, 留空提交表示保持原值
  $("f-unsplash-key").value = "";
  $("f-unsplash-key").placeholder = c.unsplash_key_set
    ? "已配置(留空保持不变), 输入新值则覆盖"
    : "输入 Access Key";
  clearKeyFlag = false;
  updateSourceVisibility();
  $("f-interval").value = c.interval_minutes;
  $("f-sorting").value = c.sorting;
  $("f-toprange").value = c.top_range;
  $("f-order").value = c.order;
  $("f-query").value = c.query || "";
  $("f-atleast").value = c.at_least || "";
  $("f-ratios").value = c.ratios || "";
  // 自定义宽高比有值且未选方向时, 下拉显示为"自定义"
  $("f-orientation").value = c.orientation ||
    (c.ratios ? "custom" : "");
  updateRatioControls();
  $("f-fit").value = c.desktop_fit;
  $("f-maxitems").value = c.cache.max_items;
  $("f-maxdays").value = c.cache.max_days;
  $("f-cachedir").value = c.cache.dir || "";
  $("f-webport").value = c.web.port;
  $("f-proxy").value = c.proxy || "";
  $("f-cat-general").checked = c.categories.general;
  $("f-cat-anime").checked = c.categories.anime;
  $("f-cat-people").checked = c.categories.people;
  $("f-purity-sfw").checked = c.purity.sfw;
  $("f-purity-sketchy").checked = c.purity.sketchy;
  $("f-target-desktop").checked = c.targets.desktop;
  $("f-target-lockscreen").checked = c.targets.lock_screen;
  $("f-autostart").checked = c.autostart;
  $("f-local-dir").value = (c.local && c.local.dir) || "";
  $("f-pause-fullscreen").checked = c.pause_when_fullscreen !== false;
  $("f-accent").checked = !!(c.theme && c.theme.accent_from_wallpaper);
  const dm = (c.theme && c.theme.dark_mode) || {};
  $("f-dark-mode").value = dm.mode || "off";
  $("f-dark-daystart").value = dm.day_start || "08:00";
  $("f-dark-nightstart").value = dm.night_start || "19:00";
  $("f-dark-lat").value = dm.latitude !== undefined ? dm.latitude : "";
  $("f-dark-lon").value = dm.longitude !== undefined ? dm.longitude : "";
  $("f-dark-dawn").value = dm.dawn_offset !== undefined ? dm.dawn_offset : 30;
  $("f-dark-dusk").value = dm.dusk_offset !== undefined ? dm.dusk_offset : 30;
  $("f-dark-systemui").checked = dm.system_ui !== false;
  updateDarkModeVisible();
  updateTopRangeVisible();
}

function collectForm() {
  return {
    revision: cfgRev,
    proxy: $("f-proxy").value.trim(),
    sources: {
      desktop: $("f-src-desktop").value,
      lock_screen: $("f-src-lock").value,
    },
    unsplash_key: $("f-unsplash-key").value.trim(),
    clear_unsplash_key: clearKeyFlag,
    interval_minutes: parseInt($("f-interval").value, 10) || 30,
    categories: {
      general: $("f-cat-general").checked,
      anime: $("f-cat-anime").checked,
      people: $("f-cat-people").checked,
    },
    purity: {
      sfw: $("f-purity-sfw").checked,
      sketchy: $("f-purity-sketchy").checked,
    },
    query: $("f-query").value.trim(),
    sorting: $("f-sorting").value,
    order: $("f-order").value,
    top_range: $("f-toprange").value,
    at_least: $("f-atleast").value.trim(),
    ratios: $("f-ratios").value.trim(),
    orientation: orientationValue(),
    targets: {
      desktop: $("f-target-desktop").checked,
      lock_screen: $("f-target-lockscreen").checked,
    },
    desktop_fit: $("f-fit").value,
    autostart: $("f-autostart").checked,
    pause_when_fullscreen: $("f-pause-fullscreen").checked,
    local: { dir: $("f-local-dir").value.trim() },
    theme: {
      accent_from_wallpaper: $("f-accent").checked,
      dark_mode: {
        mode: $("f-dark-mode").value,
        day_start: $("f-dark-daystart").value || "08:00",
        night_start: $("f-dark-nightstart").value || "19:00",
        latitude: parseFloat($("f-dark-lat").value) || 0,
        longitude: parseFloat($("f-dark-lon").value) || 0,
        dawn_offset: parseInt($("f-dark-dawn").value, 10) || 0,
        dusk_offset: parseInt($("f-dark-dusk").value, 10) || 0,
        system_ui: $("f-dark-systemui").checked,
      },
    },
    cache: {
      dir: $("f-cachedir").value.trim(),
      max_items: parseInt($("f-maxitems").value, 10) || 20,
      max_days: parseInt($("f-maxdays").value, 10) || 0,
    },
    web: { port: parseInt($("f-webport").value, 10) || 0 },
  };
}

function updateTopRangeVisible() {
  $("wrap-toprange").style.display =
    $("f-sorting").value === "toplist" ? "" : "none";
}

// 根据所选模式显示对应的设置项
function updateDarkModeVisible() {
  const mode = $("f-dark-mode").value;
  $("wrap-dark-fixed").classList.toggle("is-on", mode === "fixed");
  $("wrap-dark-sun").classList.toggle("is-on", mode === "sun");
}

// 把状态里的日出日落信息显示出来(便于确认经纬度填对没有)
function renderDarkSummary(dm) {
  const el = $("dark-summary");
  if (!el) return;
  if (!dm || !dm.enabled) {
    const mode = $("f-dark-mode").value;
    if (mode === "off") {
      el.textContent = "深浅色自动切换已关闭, 系统保持你手动设置的样式";
      return;
    }
  }
  const parts = [];
  // 零值时间(0001-01-01)表示该模式下没有日出日落, 不显示
  const hasTime = (s) => !!s && s.slice(0, 4) !== "0001";
  if (dm && hasTime(dm.sunrise)) parts.push("日出 " + dm.sunrise.slice(11, 16));
  if (dm && hasTime(dm.sunset)) parts.push("日落 " + dm.sunset.slice(11, 16));
  if (dm && hasTime(dm.next_change)) {
    // 跨日的切换要带上日期, 否则看不出是今天还是明天
    const nc = new Date(dm.next_change);
    const today = new Date();
    const sameDay =
      nc.getFullYear() === today.getFullYear() &&
      nc.getMonth() === today.getMonth() &&
      nc.getDate() === today.getDate();
    parts.push(
      "下次切换 " + (sameDay ? "" : nc.getMonth() + 1 + "月" + nc.getDate() + "日 ") +
        dm.next_change.slice(11, 16)
    );
  }
  if (dm && dm.mode !== undefined) parts.push(dm.mode === 1 ? "当前浅色" : "当前深色");
  el.textContent = parts.length ? parts.join(" · ") : "";
}

// 方向下拉为"自定义"时发送空 orientation(沿用 ratios 文本), 其余发送所选方向
function orientationValue() {
  const v = $("f-orientation").value;
  return v === "custom" ? "" : v;
}

function updateRatioControls() {
  $("wrap-ratios").style.display =
    $("f-orientation").value === "custom" ? "" : "none";
}

function fmtCountdown(ms) {
  if (ms <= 0) return "即将更换";
  const s = Math.floor(ms / 1000);
  const m = Math.floor(s / 60);
  const sec = s % 60;
  if (m >= 60) {
    const h = Math.floor(m / 60);
    return h + " 小时 " + (m % 60) + " 分";
  }
  return m + " 分 " + sec + " 秒";
}

let cfgRev = 0;
let clearKeyFlag = false;

// 按所选数据源显示/隐藏对应的专属设置卡片。
// 两个目标(桌面/锁屏)任一使用该源, 就展示该源的设置卡片。
function updateSourceVisibility() {
  const d = $("f-src-desktop").value;
  const l = $("f-src-lock").value;
  $("card-wallhaven").hidden = d !== "wallhaven" && l !== "wallhaven";
  $("card-unsplash").hidden = d !== "unsplash" && l !== "unsplash";
  $("card-local").hidden = d !== "local" && l !== "local";
}

// 读取版本信息, 有新版时显示更新提示条。
let versionBusy = false;
async function loadVersion() {
  if (versionBusy || document.hidden) return;
  versionBusy = true;
  try {
    const v = await api("/api/version");
    $("app-version").textContent = v.current || "-";
    if (v.available && v.latest) {
      $("update-text").textContent = "发现新版本 " + v.latest + " (当前 " + v.current + ")";
      $("update-link").href = v.release_url;
      $("update-banner").hidden = false;
    } else {
      $("update-banner").hidden = true;
    }
  } catch (e) {
    // 更新检查失败不影响使用, 静默忽略
  } finally {
    versionBusy = false;
  }
}

// 渲染一张壁纸的信息: 来源徽章 + ID + 标题/摄影师署名 + 原图链接。
// 桌面与锁屏各调用一次, 两个来源都能看到自己的出处。
function applyWallInfo(item, badgeId, idId, noteId, linkId) {
  const src = item.source || "wallhaven";
  const label = src === "unsplash" ? "Unsplash" : "wallhaven";
  const badge = $(badgeId);
  badge.textContent = label;
  badge.className = "src-badge src-" + src;
  $(idId).textContent = item.id || "-";
  // 备注: 优先显示摄影师署名(Unsplash 规范要求), 否则显示图片标题
  const note = $(noteId);
  if (item.author) {
    note.textContent = "Photo by " + item.author + " on Unsplash";
    note.style.display = "";
  } else if (item.title) {
    note.textContent = item.title;
    note.style.display = "";
  } else {
    note.textContent = "";
    note.style.display = "none";
  }
  const link = $(linkId);
  if (item.page_url) {
    link.href = item.page_url;
    link.textContent = "在 " + label + " 查看原图";
    link.style.display = "";
  } else {
    link.style.display = "none";
  }
}

// 状态轮询: 同一时间只允许一条请求在途, 期间收到的刷新要求合并为一次补充轮询,
// 避免换图/保存期间请求堆积。失败后退避, 成功恢复原间隔。
let statusBusy = false;
let statusAgain = false;
let statusFailures = 0;
let statusTimer = null;
const STATUS_INTERVAL = 5000;
const STATUS_TIMEOUT = 15000;

function scheduleStatus(delay) {
  if (statusTimer) {
    clearTimeout(statusTimer);
    statusTimer = null;
  }
  if (document.hidden) return; // 页面隐藏时不再安排新的轮询
  statusTimer = setTimeout(pollStatus, delay);
}

async function pollStatus() {
  if (statusBusy) {
    statusAgain = true;
    return;
  }
  statusBusy = true;
  try {
    const st = await api("/api/status", { timeoutMs: STATUS_TIMEOUT });
    await applyStatus(st);
    statusFailures = 0;
    showLinkError("");
  } catch (e) {
    statusFailures++;
    showLinkError(
      e && e.timedOut
        ? "服务响应较慢, 正在重试; 若长时间无响应请通过托盘菜单重新打开配置页"
        : "服务连接断开(可能正在重启), 恢复后将自动刷新; 若长时间无响应请通过托盘菜单重新打开配置页"
    );
  } finally {
    statusBusy = false;
    if (statusAgain) {
      statusAgain = false;
      scheduleStatus(0);
    } else {
      const delay =
        statusFailures === 0
          ? STATUS_INTERVAL
          : Math.min(STATUS_INTERVAL * Math.pow(2, statusFailures), 30000);
      scheduleStatus(delay);
    }
  }
}

// 操作完成后立即刷新一次状态。
function refreshStatus() {
  if (statusBusy) {
    statusAgain = true;
    return;
  }
  pollStatus();
}

// 错误条: 后端最近错误优先, 连接异常时显示连接提示。
function renderLastError() {
  const el = $("st-error");
  if (linkError) {
    el.textContent = linkError;
    el.hidden = false;
    return;
  }
  if (lastError) {
    el.textContent = "最近错误: " + lastError;
    el.hidden = false;
  } else {
    el.textContent = "";
    el.hidden = true;
  }
}

function showLinkError(msg) {
  linkError = msg || "";
  renderLastError();
}

async function applyStatus(st) {
  lastStatus = st;
  // 深浅色模式: 显示日出日落与下次切换时刻
  renderDarkSummary(st.dark_mode);
  // 桌面壁纸: 版本变化时才重新拉图, 解码成功后再切换, 保证信息与画面一致
  const dw = (st.current && st.current.web_image) || null;
  // 用真实尺寸预留位置, 避免图片加载时布局跳动
  if (dw && dw.width && dw.height) {
    $("preview").style.aspectRatio = dw.width + " / " + dw.height;
  }
  const dkey = dw && dw.version ? dw.version : st.current && st.current.id;
  if (st.current && st.current.id && dkey !== lastDesktopKey) {
    const url = (dw && dw.preview_url) || "/api/current-image";
    if (await swapImage($("preview"), url)) {
      lastDesktopKey = dkey;
      applyWallInfo(st.current, "badge-desktop", "cur-id", "cur-note", "cur-link");
      if (dw && dw.background_url) syncBackdrop(dw.background_url);
    }
  }
  // 锁屏壁纸: 与桌面各自独立
  const lw = (st.lock_current && st.lock_current.web_image) || null;
  if (lw && lw.width && lw.height) {
    $("lock-preview").style.aspectRatio = lw.width + " / " + lw.height;
  }
  if (st.lock_current && st.lock_current.id) {
    $("lock-preview-wrap").style.display = "";
    $("info-lock").style.display = "";
    const lkey = lw && lw.version ? lw.version : st.lock_current.id;
    if (lkey !== lastLockKey) {
      const url = (lw && lw.preview_url) || "/api/lockscreen-image";
      if (await swapImage($("lock-preview"), url)) {
        lastLockKey = lkey;
        applyWallInfo(st.lock_current, "badge-lock", "lock-id", "lock-note", "lock-link");
      }
    }
  } else {
    $("lock-preview-wrap").style.display = "none";
    $("info-lock").style.display = "none";
    lastLockKey = null;
  }
  $("st-paused").textContent = st.paused
    ? "已暂停"
    : st.fullscreen
    ? "运行中(全屏避让)"
    : "运行中";
  $("st-task").textContent = st.task_installed ? "已安装" : "未安装";
  nextRunAt = st.next_run ? new Date(st.next_run) : null;
  lastError = st.last_error || "";
  renderLastError();
  $("btn-pause").textContent = st.paused ? "继续" : "暂停";
  $("paused-badge").classList.toggle("hidden", !st.paused);
  // 历史列表: 增量更新, 未变化的条目保留原有节点
  updateHistory($("history"), st.history || [], "desktop");
  $("history-count").textContent = $("history").childElementCount;
  updateHistory($("lock-history"), st.lock_history || [], "lock");
  $("lock-history-count").textContent = $("lock-history").childElementCount;
}

// 历史列表的节点缓存: 同一张图(来源+ID+版本)复用同一个 DOM 节点,
// 避免每次轮询都重建全部缩略图(重建会导致重复下载与重复解码)。
const historyNodes = { desktop: new Map(), lock: new Map() };

function updateHistory(list, items, kind) {
  const nodes = historyNodes[kind];
  const wanted = new Map();
  const occurrences = new Map();
  items.slice(0, 12).forEach((it) => {
    if (!it.path) return;
    const wi = it.web_image || {};
    const base = (it.source || "wallhaven") + "|" + it.id + "|" + (wi.version || "");
    // 同源同 ID 可能出现多次, 用出现序号区分, 不合并历史次数
    const n = occurrences.get(base) || 0;
    occurrences.set(base, n + 1);
    wanted.set(n === 0 ? base : base + "#" + n, it);
  });
  // 移除已经不在列表中的条目
  for (const [key, node] of nodes) {
    if (!wanted.has(key)) {
      node.remove();
      nodes.delete(key);
    }
  }
  // 按目标顺序插入或复用节点
  let prev = null;
  for (const [key, it] of wanted) {
    let node = nodes.get(key);
    if (!node) {
      node = createHistoryNode(it, kind);
      nodes.set(key, node);
    }
    const target = prev ? prev.nextSibling : list.firstChild;
    if (node !== target) list.insertBefore(node, target);
    prev = node;
  }
}

// 创建一个历史缩略图节点。
function createHistoryNode(it, kind) {
  const wi = it.web_image || {};
  const li = document.createElement("li");
  const img = document.createElement("img");
  const url =
    wi.thumb_url ||
    "/api/history-image?id=" + encodeURIComponent(it.id) + (kind === "lock" ? "&type=lock" : "");
  img.src = url;
  img.loading = "lazy";
  img.decoding = "async";
  img.title = it.id;
  img.draggable = false;
  // 兜底: 派生图生成失败或文件刚被清理时标记占位,
  // 不误删仍然有效的历史记录(下一轮状态会移除真正失效的条目)
  img.onerror = function () {
    li.classList.add("is-broken");
  };
  // 双击查看高清大图所需的数据
  const src = it.source || "wallhaven";
  li.dataset.id = it.id;
  li.dataset.source = src;
  li.dataset.version = wi.version || "";
  li.dataset.type = kind;
  li.dataset.page = it.page_url || "";
  li.dataset.note = it.author
    ? "Photo by " + it.author + " on Unsplash"
    : it.title || "";
  const meta = document.createElement("div");
  meta.className = "meta";
  // 来源徽章: 桌面与锁屏历史混排时也能一眼看出出处
  const badge = document.createElement("span");
  badge.className = "src-badge src-xs src-" + src;
  badge.textContent = src === "unsplash" ? "Unsplash" : "wallhaven";
  meta.appendChild(badge);
  const idEl = document.createElement("span");
  idEl.className = "mono";
  idEl.textContent = it.id;
  meta.appendChild(idEl);
  li.appendChild(img);
  li.appendChild(meta);
  return li;
}

/* ---------- 高清大图浮层 ---------- */

// lastStatus 保存最近一次状态, 供双击预览图时取当前壁纸信息。
let lastStatus = null;
let lightboxOpen = false;
// lightboxToken 用于作废过期的关闭回调(避免快速开关时误隐藏)
let lightboxToken = 0;
// lightboxSourceRect 记录来源元素位置, 图片由此处展开/收回
let lightboxSourceRect = null;

// 动画节奏: 展开 520ms 走"先慢后快再回落"的曲线, 观感上是逐渐撑开而不是瞬间到位;
// 收回 340ms 走"起步快、收尾稳"的曲线, 保证可见段就有明显位移(用 ease-in 会看起来像没动)。
const LIGHTBOX_EASE = "cubic-bezier(.3,0,.4,1)";
const LIGHTBOX_SETTLE_EASE = "cubic-bezier(.2,.8,.3,1)";
const LIGHTBOX_CLOSE_EASE = "cubic-bezier(.25,.9,.35,1)";
const LIGHTBOX_EXPAND_MS = 520;
const LIGHTBOX_CLOSE_MS = 340;

// lockScroll 锁定页面滚动, 并用 padding-right 补回滚动条宽度:
// 否则滚动条消失会让整页内容横向跳一下, 观感上像闪屏。
function lockScroll() {
  const doc = document.documentElement;
  const sbw = window.innerWidth - doc.clientWidth;
  if (sbw > 0) {
    doc.style.paddingRight = sbw + "px";
  }
  doc.classList.add("lightbox-on");
}

// unlockScroll 解除滚动锁定并还原 padding。
function unlockScroll() {
  const doc = document.documentElement;
  doc.style.paddingRight = "";
  doc.classList.remove("lightbox-on");
}

// 用户偏好减少动效时不做展开/收回动画
function prefersReducedMotion() {
  return (
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

// sourceTransform 计算"从来源元素位置"到"目标位置"的变换。
function sourceTransform(sourceRect, targetRect) {
  if (!sourceRect || !targetRect || !targetRect.width || !targetRect.height) {
    return null;
  }
  const sx = sourceRect.width / targetRect.width;
  const sy = sourceRect.height / targetRect.height;
  const tx =
    sourceRect.left + sourceRect.width / 2 - (targetRect.left + targetRect.width / 2);
  const ty =
    sourceRect.top + sourceRect.height / 2 - (targetRect.top + targetRect.height / 2);
  return "translate(" + tx + "px, " + ty + "px) scale(" + sx + ", " + sy + ")";
}

// openLightbox 打开高清大图。
// opts: url/id/source/note/pageUrl; sourceEl: 触发双击的元素, 图片从它所在位置展开。
function openLightbox(opts, sourceEl) {
  if (!opts || !opts.url) return;
  const el = $("lightbox");
  const img = $("lightbox-img");
  const src = opts.source || "wallhaven";
  const label = src === "unsplash" ? "Unsplash" : "wallhaven";

  const badge = $("lightbox-badge");
  badge.textContent = label;
  badge.className = "src-badge src-" + src;
  $("lightbox-id").textContent = opts.id || "-";
  $("lightbox-size").textContent = "";
  const note = $("lightbox-note");
  if (opts.note) {
    note.textContent = opts.note;
    note.style.display = "";
  } else {
    note.textContent = "";
    note.style.display = "none";
  }
  const link = $("lightbox-link");
  if (opts.pageUrl) {
    link.href = opts.pageUrl;
    link.style.display = "";
  } else {
    link.style.display = "none";
  }

  lightboxToken++;
  // 记录来源位置: 图片从被双击的缩略图/预览图处展开
  lightboxSourceRect = sourceEl ? sourceEl.getBoundingClientRect() : null;

  $("lightbox-loading").hidden = false;
  img.hidden = true;
  img.draggable = false;
  el.hidden = false;
  el.classList.remove("is-closing");
  lightboxOpen = true;
  lockScroll();
  // 遮罩淡入: 先落初始态再触发过渡
  el.classList.remove("is-visible");
  void el.offsetWidth;
  requestAnimationFrame(function () {
    el.classList.add("is-visible");
  });

  img.onload = function () {
    $("lightbox-loading").hidden = true;
    this.hidden = false;
    $("lightbox-size").textContent = this.naturalWidth + " × " + this.naturalHeight;
    // 图逐渐展开: 从来源位置放大到居中大图。
    // 起步快、收尾稳(expo-out), 末段轻微过冲后回落, 让"展开"更有力度。
    if (prefersReducedMotion()) return;
    const from = sourceTransform(lightboxSourceRect, this.getBoundingClientRect());
    if (!from) {
      this.animate(
        [
          { opacity: 0, transform: "scale(.88)", filter: "blur(12px)" },
          { opacity: 1, transform: "none", filter: "blur(0px)" }
        ],
        { duration: LIGHTBOX_EXPAND_MS, easing: LIGHTBOX_EASE, fill: "backwards" }
      );      return;
    }
    this.animate(
      [
        {
          opacity: 0,
          transform: from,
          filter: "blur(14px)",
          easing: LIGHTBOX_EASE
        },
        {
          opacity: 1,
          transform: "scale(1.016)",
          filter: "blur(0px)",
          offset: 0.68,
          easing: LIGHTBOX_SETTLE_EASE
        },
        { opacity: 1, transform: "none", filter: "blur(0px)" }
      ],
      { duration: LIGHTBOX_EXPAND_MS, fill: "backwards" }
    );
  };
  img.onerror = function () {
    $("lightbox-loading").hidden = true;
    showMsg("原图加载失败, 可能已被缓存清理", true);
    closeLightbox();
  };
  img.src = opts.url;
}

// closeLightbox 关闭高清大图, 图片先收回来源位置再隐藏。
function closeLightbox() {
  if (!lightboxOpen) return;
  lightboxOpen = false;
  lightboxToken++;
  const token = lightboxToken;
  const el = $("lightbox");
  const img = $("lightbox-img");
  el.classList.add("is-closing");
  el.classList.remove("is-visible");

  const finish = function () {
    if (token !== lightboxToken) return;
    el.hidden = true;
    el.classList.remove("is-closing");
    img.removeAttribute("src");
    img.onload = null;
    img.onerror = null;
    unlockScroll();
  };

  const to = prefersReducedMotion()
    ? null
    : sourceTransform(lightboxSourceRect, img.getBoundingClientRect());
  if (to) {
    const anim = img.animate(
      [
        { opacity: 1, transform: "none", filter: "blur(0px)" },
        { opacity: 0.2, transform: to, filter: "blur(8px)" }
      ],
      { duration: LIGHTBOX_CLOSE_MS, easing: LIGHTBOX_CLOSE_EASE }
    );
    anim.onfinish = finish;
    // 兜底: 动画被打断时也要完成关闭
    setTimeout(finish, LIGHTBOX_CLOSE_MS + 180);
  } else {
    finish();
  }
}

// openCurrentLightbox 打开当前桌面或锁屏壁纸的原图。
function openCurrentLightbox(kind, sourceEl) {
  const st = lastStatus;
  if (!st) return;
  const item = kind === "lock" ? st.lock_current : st.current;
  if (!item || !item.id) return;
  openLightbox(
    {
      url: kind === "lock" ? "/api/lockscreen-image" : "/api/current-image",
      id: item.id,
      source: item.source || "wallhaven",
      note: item.author
        ? "Photo by " + item.author + " on Unsplash"
        : item.title || "",
      pageUrl: item.page_url || ""
    },
    sourceEl
  );
}

// bindHistoryLightbox 给历史列表绑定双击查看大图(事件委托, 节点增删不影响)。
function bindHistoryLightbox(listEl, kind) {
  listEl.addEventListener("dblclick", function (e) {
    const li = e.target.closest("li");
    if (!li || !li.dataset.id) return;
    const q =
      "?id=" + encodeURIComponent(li.dataset.id) +
      "&source=" + encodeURIComponent(li.dataset.source || "") +
      "&type=" + kind;
    const v = li.dataset.version ? "&v=" + encodeURIComponent(li.dataset.version) : "";
    openLightbox(
      {
        url: "/api/history-image" + q + v,
        id: li.dataset.id,
        source: li.dataset.source || "wallhaven",
        note: li.dataset.note || "",
        pageUrl: li.dataset.page || ""
      },
      e.target
    );
  });
}

// 自绘下拉: 原生 select 视觉隐藏但保留可访问性与取值, 菜单完全自绘。
// 这样展开面板不再由系统绘制, 圆角/玻璃/高亮都能控制。
const selMenuHosts = [];

function enhanceSelect(sel) {
  if (sel.dataset.enhanced) return;
  sel.dataset.enhanced = "1";

  const wrap = document.createElement("div");
  wrap.className = "sel";
  sel.parentNode.insertBefore(wrap, sel);
  wrap.appendChild(sel);
  sel.classList.add("sel-native");

  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "sel-btn";
  const label = document.createElement("span");
  label.className = "sel-btn-label";
  const caret = document.createElement("span");
  caret.className = "sel-caret";
  btn.appendChild(label);
  btn.appendChild(caret);
  wrap.appendChild(btn);

  const menu = document.createElement("div");
  menu.className = "sel-menu";
  menu.setAttribute("role", "listbox");
  wrap.appendChild(menu);
  selMenuHosts.push(wrap);

  const opts = [];
  function buildOptions() {
    menu.innerHTML = "";
    opts.length = 0;
    for (const opt of sel.options) {
      if (opt.hidden) continue;
      const item = document.createElement("div");
      item.className = "sel-opt";
      item.setAttribute("role", "option");
      item.textContent = opt.textContent;
      item.addEventListener("click", () => {
        sel.value = opt.value;
        sel.dispatchEvent(new Event("change"));
        closeMenu();
      });
      menu.appendChild(item);
      opts.push({ item, value: opt.value });
    }
    paint();
  }

  function paint() {
    opts.forEach((o) => {
      const on = o.value === sel.value;
      o.item.classList.toggle("is-selected", on);
      o.item.setAttribute("aria-selected", on ? "true" : "false");
    });
    const cur = sel.options[sel.selectedIndex];
    label.textContent = cur ? cur.textContent : "";
  }

  let activeIdx = -1;
  function setActive(i) {
    activeIdx = i;
    opts.forEach((o, n) => o.item.classList.toggle("is-active", n === i));
    if (i >= 0 && opts[i]) opts[i].item.scrollIntoView({ block: "nearest" });
  }

  function openMenu() {
    closeAllMenus();
    wrap.classList.add("is-open");
    setActive(opts.findIndex((o) => o.value === sel.value));
  }

  function closeMenu() {
    wrap.classList.remove("is-open");
    setActive(-1);
  }

  btn.addEventListener("click", () => {
    wrap.classList.contains("is-open") ? closeMenu() : openMenu();
  });
  sel.addEventListener("change", () => {
    paint();
    closeMenu();
  });
  btn.addEventListener("keydown", onKey);
  sel.addEventListener("keydown", onKey);

  function onKey(e) {
    if (!wrap.classList.contains("is-open")) {
      if (e.key === "ArrowDown" || e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        openMenu();
      }
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      closeMenu();
      btn.focus();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(Math.min(activeIdx + 1, opts.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(activeIdx - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (opts[activeIdx]) opts[activeIdx].item.click();
    }
  }

  buildOptions();
  btn.tabIndex = 0;
}

function closeAllMenus() {
  selMenuHosts.forEach((w) => w.classList.remove("is-open"));
}

document.addEventListener("click", (e) => {
  if (!e.target.closest(".sel")) closeAllMenus();
});

// 自绘数字步进器: 原生 spinner 由系统绘制不可控, 隐藏后自绘,
// 加减行为仍走原生 stepUp/stepDown, 方向键与滚轮行为不变。
function enhanceNumber(input) {
  if (input.dataset.enhanced) return;
  input.dataset.enhanced = "1";

  const wrap = document.createElement("div");
  wrap.className = "num-wrap";
  input.parentNode.insertBefore(wrap, input);
  wrap.appendChild(input);

  const step = document.createElement("div");
  step.className = "num-step";
  const up = document.createElement("button");
  up.type = "button";
  up.className = "num-btn num-up";
  up.setAttribute("aria-label", "增加");
  const down = document.createElement("button");
  down.type = "button";
  down.className = "num-btn num-down";
  down.setAttribute("aria-label", "减少");
  step.appendChild(up);
  step.appendChild(down);
  wrap.appendChild(step);

  const sync = () => {
    const v = input.valueAsNumber;
    up.disabled = input.max !== "" && v >= Number(input.max);
    down.disabled = input.min !== "" && v <= Number(input.min);
  };
  up.addEventListener("click", () => {
    input.stepUp();
    sync();
  });
  down.addEventListener("click", () => {
    input.stepDown();
    sync();
  });
  input.addEventListener("input", sync);
  input.addEventListener("change", sync);
  sync();
}

// fillForm 设置 select.value 后同步自绘按钮的文字
function syncSelectLabels() {
  document.querySelectorAll("select[data-enhanced]").forEach((sel) => {
    const l = sel.parentNode.querySelector(".sel-btn-label");
    if (!l) return;
    const cur = sel.options[sel.selectedIndex];
    l.textContent = cur ? cur.textContent : "";
    sel.parentNode
      .querySelectorAll(".sel-opt")
      .forEach((item, i) => {
        const on = sel.options[i] && sel.options[i].value === sel.value;
        item.classList.toggle("is-selected", !!on);
      });
  });
}

async function loadConfig() {
  try {
    const c = await api("/api/config");
    fillForm(c);
  } catch (e) {
    showMsg("读取配置失败: " + e.message, true);
  }
}

function showMsg(text, isErr) {
  const el = $("save-msg");
  el.textContent = text;
  el.className = isErr ? "error" : "msg";
  el.hidden = !text;
}

async function saveConfig(ev) {
  ev.preventDefault();
  $("btn-save").disabled = true;
  showMsg("保存中...", false);
  try {
    const saved = await api("/api/config", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(collectForm()),
    });
    fillForm(saved);
    showMsg("已保存" + (saved.targets.lock_screen ? " (如首次开启锁屏, 请在 UAC 弹窗中允许)" : ""), false);
    refreshStatus();
  } catch (e) {
    showMsg("保存失败: " + e.message, true);
  } finally {
    $("btn-save").disabled = false;
  }
}

async function action(path, btn) {
  if (btn) btn.disabled = true;
  try {
    await api(path, { method: "POST" });
    showMsg("操作成功", false);
    refreshStatus();
  } catch (e) {
    showMsg("操作失败: " + e.message, true);
  } finally {
    if (btn) btn.disabled = false;
  }
}

async function removeLockScreen() {
  if (!confirm("确定移除锁屏策略吗? 将删除计划任务并清除注册表设置(需要 UAC 确认)。")) return;
  const btn = $("btn-remove-lockscreen");
  btn.disabled = true;
  try {
    await api("/api/lockscreen/remove", { method: "POST" });
    showMsg("锁屏策略已移除", false);
    refreshStatus();
  } catch (e) {
    showMsg("移除失败: " + e.message, true);
  } finally {
    btn.disabled = false;
  }
}

$("cfg-form").addEventListener("submit", saveConfig);
$("f-sorting").addEventListener("change", updateTopRangeVisible);
$("f-dark-mode").addEventListener("change", updateDarkModeVisible);
$("f-dark-city").addEventListener("change", (e) => {
  const v = e.target.value;
  if (!v) return;
  const [lat, lon] = v.split(",");
  $("f-dark-lat").value = lat;
  $("f-dark-lon").value = lon;
  e.target.value = "";
  showMsg("已填入城市坐标, 点击保存设置生效", false);
});
$("btn-geo").addEventListener("click", async (e) => {
  const btn = e.target;
  btn.disabled = true;
  const old = btn.textContent;
  btn.textContent = "检测中…";
  try {
    const r = await api("/api/theme/geo");
    $("f-dark-lat").value = Number(r.latitude).toFixed(4);
    $("f-dark-lon").value = Number(r.longitude).toFixed(4);
    showMsg(
      "已检测到" + (r.city || "当前位置") + " 的坐标, 点击保存设置生效",
      false
    );
  } catch (err) {
    showMsg("位置检测失败: " + err.message, true);
  } finally {
    btn.textContent = old;
    btn.disabled = false;
  }
});
$("f-orientation").addEventListener("change", updateRatioControls);
$("f-src-desktop").addEventListener("change", updateSourceVisibility);
$("f-src-lock").addEventListener("change", updateSourceVisibility);
$("f-src-lock").addEventListener("change", updateSourceVisibility);
$("btn-clear-key").addEventListener("click", () => {
  if (!confirm("确定清除已保存的 Unsplash Access Key 吗?")) return;
  clearKeyFlag = true;
  $("f-unsplash-key").value = "";
  $("f-unsplash-key").placeholder = "保存后将被清除";
  showMsg("已标记清除, 点击保存设置生效", false);
});
$("preview").onerror = function () { this.style.display = "none"; };
$("lock-preview").onerror = function () { this.style.display = "none"; };
$("btn-next").addEventListener("click", (e) => action("/api/next", e.target));
$("btn-prev").addEventListener("click", (e) => action("/api/prev", e.target));
$("btn-pause").addEventListener("click", (e) => {
  action($("btn-pause").textContent === "暂停" ? "/api/pause" : "/api/resume", e.target);
});
$("btn-remove-lockscreen").addEventListener("click", removeLockScreen);

setInterval(() => {
  if (document.hidden) return;
  if (!nextRunAt) { $("st-next").textContent = "-"; return; }
  const ms = nextRunAt.getTime() - Date.now();
  $("st-next").textContent = fmtCountdown(ms);
}, 1000);

document.querySelectorAll("select").forEach(enhanceSelect);
document.querySelectorAll('input[type="number"]').forEach(enhanceNumber);
syncSelectLabels();

// 高清大图: 双击预览图或历史缩略图
$("preview").addEventListener("dblclick", (e) => openCurrentLightbox("desktop", e.currentTarget));
$("lock-preview").addEventListener("dblclick", (e) => openCurrentLightbox("lock", e.currentTarget));
bindHistoryLightbox($("history"), "desktop");
bindHistoryLightbox($("lock-history"), "lock");
$("lightbox-close").addEventListener("click", closeLightbox);
// 点击图片以外的区域关闭
$("lightbox").addEventListener("click", function (e) {
  if (e.target === this || e.target.classList.contains("lightbox-stage")) closeLightbox();
});
document.addEventListener("keydown", function (e) {
  if (e.key === "Escape") closeLightbox();
});
// 禁止复制/另存高清大图: 右键菜单、拖拽、复制事件一律拦截
$("lightbox").addEventListener("contextmenu", (e) => e.preventDefault());
$("lightbox").addEventListener("dragstart", (e) => e.preventDefault());
$("lightbox").addEventListener("copy", (e) => e.preventDefault());
// 页面内的静态预览图不可拖拽(避免拖到别的程序即等于复制)
document.querySelectorAll(".preview img").forEach((img) => {
  img.draggable = false;
});

loadConfig();
pollStatus();
loadVersion();
scheduleStatus(STATUS_INTERVAL);
// 更新检查频率低: 每 30 分钟一次即可
setInterval(loadVersion, 30 * 60 * 1000);

// 页面隐藏时暂停轮询与倒计时, 恢复显示后立即同步一次
document.addEventListener("visibilitychange", () => {
  if (document.hidden) {
    if (statusTimer) {
      clearTimeout(statusTimer);
      statusTimer = null;
    }
    return;
  }
  pollStatus();
});
