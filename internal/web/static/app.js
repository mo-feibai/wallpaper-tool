const $ = (id) => document.getElementById(id);

let nextRunAt = null;
let lastDesktopId = null;
let lastLockId = null;

async function api(path, opts) {
  const resp = await fetch(path, opts);
  if (!resp.ok) {
    let msg = await resp.text();
    throw new Error(msg || ("HTTP " + resp.status));
  }
  return resp.json();
}

// 用变化的查询参数强制浏览器重新拉图, 实现换图后预览自动同步
function refreshImage(imgEl, url) {
  imgEl.style.display = "";
  imgEl.src = url + "?t=" + Date.now();
}

function fillForm(c) {
  cfgRev = c.revision || 0;
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
  updateTopRangeVisible();
}

function collectForm() {
  return {
    revision: cfgRev,
    proxy: $("f-proxy").value.trim(),
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

async function loadStatus() {
  try {
    const st = await api("/api/status");
    $("cur-id").textContent = st.current.id || "-";
    $("lock-id").textContent = st.lock_current.id || "-";
    // 桌面预览: ID 变化时才重新拉图, 避免无谓刷新
    if (st.current.id && st.current.id !== lastDesktopId) {
      lastDesktopId = st.current.id;
      refreshImage($("preview"), "/api/current-image");
    }
    // 锁屏预览: 有锁屏图时显示区块, ID 变化时重新拉图
    if (st.lock_current.id) {
      $("lock-preview-wrap").style.display = "";
      if (st.lock_current.id !== lastLockId) {
        lastLockId = st.lock_current.id;
        refreshImage($("lock-preview"), "/api/lockscreen-image");
      }
    }
    $("st-paused").textContent = st.paused ? "已暂停" : "运行中";
    $("st-task").textContent = st.task_installed ? "已安装" : "未安装";
    nextRunAt = st.next_run ? new Date(st.next_run) : null;
    if (st.last_error) {
      $("st-error").textContent = "最近错误: " + st.last_error;
    } else {
      $("st-error").textContent = "";
    }
    const link = $("cur-link");
    if (st.current.page_url) {
      link.href = st.current.page_url;
      link.style.display = "";
    } else {
      link.style.display = "none";
    }
    $("btn-pause").textContent = st.paused ? "继续" : "暂停";
    $("paused-badge").classList.toggle("hidden", !st.paused);
    const list = $("history");
    list.innerHTML = "";
    renderHistory(list, st.history || []);
    $("history-count").textContent = $("history").children.length;
    const lockList = $("lock-history");
    lockList.innerHTML = "";
    renderHistory(lockList, st.lock_history || [], "lock");
    $("lock-history-count").textContent = $("lock-history").children.length;
  } catch (e) {
    // fetch 失败通常是服务正在重启; 端口固定时轮询会自动恢复连接
    $("st-error").textContent =
      "服务连接断开(可能正在重启), 恢复后将自动刷新; 若长时间无响应请通过托盘菜单重新打开配置页";
  }
}

// 渲染单个历史列表(最多 12 张, 已由服务端过滤掉文件不存在的条目)。
// type 为 "lock" 时缩略图走锁屏历史接口。
function renderHistory(list, items, type) {
  items.slice(0, 12).forEach((it) => {
    if (!it.path) return;
    const li = document.createElement("li");
    const img = document.createElement("img");
    let url = "/api/history-image?id=" + encodeURIComponent(it.id);
    if (type === "lock") url += "&type=lock";
    img.src = url;
    img.loading = "lazy";
    img.title = it.id;
    // 兜底: 状态轮询与图片加载之间文件可能刚被清理, 加载失败时隐藏整条
    img.onerror = function () { li.style.display = "none"; };
    const meta = document.createElement("div");
    meta.className = "meta";
    meta.textContent = it.id;
    li.appendChild(img);
    li.appendChild(meta);
    list.appendChild(li);
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
    loadStatus();
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
    loadStatus();
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
    loadStatus();
  } catch (e) {
    showMsg("移除失败: " + e.message, true);
  } finally {
    btn.disabled = false;
  }
}

$("cfg-form").addEventListener("submit", saveConfig);
$("f-sorting").addEventListener("change", updateTopRangeVisible);
$("f-orientation").addEventListener("change", updateRatioControls);
$("preview").onerror = function () { this.style.display = "none"; };
$("lock-preview").onerror = function () { this.style.display = "none"; };
$("btn-next").addEventListener("click", (e) => action("/api/next", e.target));
$("btn-prev").addEventListener("click", (e) => action("/api/prev", e.target));
$("btn-pause").addEventListener("click", (e) => {
  action($("btn-pause").textContent === "暂停" ? "/api/pause" : "/api/resume", e.target);
});
$("btn-remove-lockscreen").addEventListener("click", removeLockScreen);

setInterval(() => {
  if (!nextRunAt) { $("st-next").textContent = "-"; return; }
  const ms = nextRunAt.getTime() - Date.now();
  $("st-next").textContent = fmtCountdown(ms);
}, 1000);

loadConfig();
loadStatus();
setInterval(loadStatus, 5000);
