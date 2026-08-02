"use strict";
document.documentElement.classList.add("js");

/* Shared helpers; per-page init no-ops when its elements are absent. */

/* Theme (UI 主题化): 默认跟随系统深浅色; 手动切换持久化到 localStorage
   "wbot-theme", 之后始终用该偏好(直到手动切回)。按钮 icon 显示"切换后"
   的主题(浅色界面显示 🌙 → 点按进深色)。 */
function initTheme() {
  const btn = document.getElementById("theme-toggle");
  if (!btn) return;
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  const saved = localStorage.getItem("wbot-theme");
  const apply = (theme) => {
    document.documentElement.dataset.theme = theme;
    btn.textContent = theme === "dark" ? "☀️" : "🌙";
    btn.setAttribute("aria-label", theme === "dark" ? "切换到浅色主题" : "切换到深色主题");
    redrawCharts(); /* 主题切换后立即重绘图表(颜色随 token 走) */
  };
  apply(saved || (media.matches ? "dark" : "light"));
  btn.addEventListener("click", () => {
    const next = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
    localStorage.setItem("wbot-theme", next);
    apply(next);
  });
  /* 未手动选择过时跟随系统切换 */
  if (!saved) media.addEventListener("change", (e) => apply(e.matches ? "dark" : "light"));
}

async function fetchJSON(url, opts) {
  let resp;
  try {
    resp = await fetch(url, opts);
  } catch (err) {
    throw new Error("cannot reach the server");
  }
  if (!resp.ok) {
    let msg = "HTTP " + resp.status;
    try {
      const body = await resp.json();
      if (body && typeof body.message === "string") {
        /* API-wide error convention: {code, message, action} */
        msg = body.message;
        if (typeof body.action === "string" && body.action !== "") {
          msg += " · " + body.action;
        }
      } else if (body && typeof body.error === "string") {
        msg = body.error;
      }
    } catch (err) {
      /* error body was not JSON, keep the status line */
    }
    throw new Error(msg);
  }
  try {
    return await resp.json();
  } catch (err) {
    throw new Error("unexpected server response");
  }
}

function showError(el, err) {
  el.textContent = err && err.message ? err.message : String(err);
  el.hidden = false;
}

function clearError(el) {
  el.hidden = true;
  el.textContent = "";
}

function setText(id, text) {
  document.getElementById(id).textContent = text;
}

function appendRow(tbody, cells) {
  const tr = document.createElement("tr");
  for (const cell of cells) {
    /* 单元格可为字符串或预构建的 td 元素(如 side-buy 语义色列)。
       此前对元素走 textContent 赋值会被字符串化成
       "[object HTMLTableDataCellElement]",导致带 class 的列渲染失效。 */
    tr.appendChild(cell instanceof Node ? cell : (() => {
      const td = document.createElement("td");
      td.textContent = cell;
      return td;
    })());
  }
  tbody.appendChild(tr);
}

function renderTable(id, rows) {
  const table = document.getElementById(id);
  const empty = document.getElementById(id.replace("-table", "-empty"));
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  if (rows.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    return;
  }
  for (const row of rows) {
    appendRow(tbody, row);
  }
  empty.hidden = true;
  table.hidden = false;
}

async function loadJSON(url, errorEl, render) {
  try {
    const data = await fetchJSON(url);
    clearError(errorEl);
    render(data);
  } catch (err) {
    showError(errorEl, err);
  }
}

/* Dashboard page (老板 2026-08-02: Data 页改 Dashboard): 账户聚合 + 子账户明细
   + 订单状态。模拟盘默认(安全红线);实盘只读。bars/quote 区块已删除。 */

const FUTU_ENVS = [
  {key: "sim", label: "Paper 模拟盘"},
  {key: "real", label: "实盘 Live"},
];

function envLabel(key) {
  return key === "real" ? "实盘" : "模拟盘";
}

function fmtAccountMoney(v) {
  return Number(v).toLocaleString("en-US", {maximumFractionDigits: 2});
}

/* ISO 时间戳 → 本地时间 "YYYY-MM-DD HH:MM"(券商面板惯例;空值/非法值
   原样兜底,排序不受影响——排序用的是原始字段值,仅显示层转换)。 */
function fmtTime(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return String(iso);
  const p = (n) => String(n).padStart(2, "0");
  return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
}

/* 数据新鲜度:auto-refresh 页每次成功刷新后打点「更新于 HH:MM:SS」,
   让用户一眼判断数据是否陈旧(券商面板惯例;也验证轮询还活着)。 */
function fmtClock(d) {
  const p = (n) => String(n).padStart(2, "0");
  return p(d.getHours()) + ":" + p(d.getMinutes()) + ":" + p(d.getSeconds());
}
function stampUpdated(id) {
  const el = document.getElementById(id);
  if (el) el.textContent = "更新于 " + fmtClock(new Date());
}

let dashEnv = "sim"; /* 当前明细视图(聚合卡/持仓/订单) */
const snapByEnv = {sim: null, real: null};
const errByEnv = {sim: null, real: null};
const curveByEnv = {sim: null, real: null}; /* 资产曲线点序(chronological) */

/* 子账户:sim + real 各查一次;失败只影响该行,双失败时顶部横幅提示。 */
async function loadEnvSnap(env) {
  try {
    snapByEnv[env] = await fetchJSON("/v1/futu/account?env=" + env);
    errByEnv[env] = null;
  } catch (err) {
    snapByEnv[env] = null;
    errByEnv[env] = err;
  }
}

/* 资产曲线:DB 快照序列(`wbot ingest account` 写入);失败静默(次要视图,
   不破坏聚合卡)。 */
async function loadSnapSeries(env) {
  try {
    curveByEnv[env] = await fetchJSON("/v1/account/snapshots?env=" + env + "&limit=120");
  } catch (err) {
    curveByEnv[env] = null;
  }
}

function renderBadge() {
  const paper = dashEnv === "sim";
  const badge = document.getElementById("env-badge");
  badge.className = "env-badge " + (paper ? "paper" : "real");
  badge.textContent = paper ? "PAPER · 模拟盘" : "REAL · 实盘";
  for (const btn of [document.getElementById("env-paper"), document.getElementById("env-real")]) {
    btn.classList.toggle("active", btn.id === (paper ? "env-paper" : "env-real"));
  }
  for (const tag of ["summary-env", "positions-env", "orders-env"]) {
    document.getElementById(tag).textContent = envLabel(dashEnv);
  }
}

function renderSummary() {
  const errorEl = document.getElementById("summary-error");
  const cards = document.getElementById("summary-cards");
  if (errByEnv[dashEnv]) {
    cards.hidden = true;
    showError(errorEl, errByEnv[dashEnv]);
    return;
  }
  clearError(errorEl);
  const f = snapByEnv[dashEnv].funds;
  setText("summary-total-assets", fmtAccountMoney(f.total_assets));
  setText("summary-cash", fmtAccountMoney(f.cash));
  setText("summary-market-val", fmtAccountMoney(f.market_val));
  setText("summary-power", fmtAccountMoney(f.power));
  setText("summary-available-cash", fmtAccountMoney(f.available_cash));
  cards.hidden = false;
  renderSummaryCurve();
}

/* 资产曲线:≥2 个点画 sparkline(total_assets 时序,drawSparkline 读
   chronological),否则显示引导提示(可 `wbot ingest account` 开始记录)。 */
function renderSummaryCurve() {
  const wrap = document.getElementById("summary-curve-wrap");
  const canvas = document.getElementById("summary-canvas");
  const empty = document.getElementById("summary-curve-empty");
  const range = document.getElementById("summary-curve-range");
  const series = curveByEnv[dashEnv];
  if (!series || !series.points || series.points.length < 2) {
    wrap.hidden = false;
    canvas.hidden = true;
    empty.hidden = false;
    setText(range, "");
    return;
  }
  const pts = series.points;
  canvas.hidden = false;
  empty.hidden = true;
  const first = new Date(pts[0].captured_at);
  const last = new Date(pts[pts.length - 1].captured_at);
  setText(range, fmtClock(first) + " → " + fmtClock(last) + " · " + pts.length + " 点");
  drawSparkline(canvas, pts.map((p) => p.total_assets));
  attachCurveHover(canvas, pts);
}

/* 资产曲线读数(富途/IB 账户曲线惯例):鼠标悬停/触摸显示该快照时刻与
   总资产,移出/松手隐藏。x 坐标按比例映射到最近数据点;touch 即触即显
   (移动端无 hover 概念),preventDefault 让读数优先于页面滚动。 */
function attachCurveHover(canvas, pts) {
  const tip = document.getElementById("summary-curve-tip");
  if (!tip) return;
  const showTip = (clientX) => {
    const rect = canvas.getBoundingClientRect();
    const x = clientX - rect.left;
    const idx = Math.max(0, Math.min(pts.length - 1,
      Math.round((x / rect.width) * (pts.length - 1))));
    const p = pts[idx];
    tip.textContent = fmtTime(p.captured_at) + " · " + fmtAccountMoney(p.total_assets);
    tip.hidden = false;
    tip.style.left = Math.min(x + 10, rect.width - tip.offsetWidth - 4) + "px";
    tip.style.top = Math.max(0, rect.height - tip.offsetHeight - 8) + "px";
  };
  const hideTip = () => { tip.hidden = true; };
  canvas.addEventListener("mousemove", (e) => showTip(e.clientX));
  canvas.addEventListener("mouseleave", hideTip);
  canvas.addEventListener("touchstart", (e) => { e.preventDefault(); showTip(e.touches[0].clientX); }, {passive: false});
  canvas.addEventListener("touchmove", (e) => { e.preventDefault(); showTip(e.touches[0].clientX); }, {passive: false});
  canvas.addEventListener("touchend", hideTip);
}

function renderAccounts() {
  const table = document.getElementById("accounts-table");
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  for (const env of FUTU_ENVS) {
    const tr = document.createElement("tr");
    const snap = snapByEnv[env.key];
    const err = errByEnv[env.key];
    if (snap) {
      const cells = [env.label, snap.acc_id,
        fmtAccountMoney(snap.funds.total_assets), fmtAccountMoney(snap.funds.cash),
        fmtAccountMoney(snap.funds.market_val), fmtAccountMoney(snap.funds.available_cash),
        fmtAccountMoney(snap.funds.power), String(snap.positions.length), "可用"];
      for (const c of cells) {
        const td = document.createElement("td");
        td.textContent = c;
        tr.appendChild(td);
      }
    } else {
      for (const c of [env.label, "—", "—", "—", "—", "—", "—", "—"]) {
        const td = document.createElement("td");
        td.textContent = c;
        tr.appendChild(td);
      }
      const td = document.createElement("td");
      if (err) {
        td.textContent = "不可用";
        td.className = "state-down";
        td.title = String(err);
      } else {
        td.textContent = "加载中";
      }
      tr.appendChild(td);
    }
    tbody.appendChild(tr);
  }
  table.hidden = false;
}

function renderPositions() {
  const snap = snapByEnv[dashEnv];
  const errorEl = document.getElementById("positions-error");
  const empty = document.getElementById("positions-empty");
  if (errByEnv[dashEnv]) {
    showError(errorEl, errByEnv[dashEnv]);
    return;
  }
  clearError(errorEl);
  if (!snap || snap.positions.length === 0) {
    empty.hidden = false;
    return;
  }
  empty.hidden = true;
  const table = document.getElementById("positions-table");
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  const list = positionsSorter ? positionsSorter.sortItems(snap.positions) : snap.positions;
  for (const p of list) {
    const tr = document.createElement("tr");
    for (const c of [p.symbol, p.qty, p.avg_cost, p.price, p.market_val]) {
      const td = document.createElement("td");
      td.textContent = c;
      tr.appendChild(td);
    }
    tr.appendChild(numCell(p.pl));
    tbody.appendChild(tr);
  }
  table.hidden = false;
}

/* 盈亏/收益率语义色(券商 UI 惯例):正值 --ok 绿、负值 --down 红。
   v 为原始数值(未格式化),着色以原始值为准,显示用可选 fmt 格式化。 */
function numCell(v, fmt) {
  const td = document.createElement("td");
  td.textContent = fmt ? fmt(v) : v;
  if (typeof v === "number" && !isNaN(v)) {
    td.className = v > 0 ? "num-up" : v < 0 ? "num-down" : "";
  }
  return td;
}

/* 动态枚举值中文化(2026-08-02):方向 buy/sell、runs 状态值由 API 返回
   英文,展示转中文;未知值原样透传。 */
const SIDE_ZH = {buy: "买入", sell: "卖出"};
const STATUS_ZH = {succeeded: "成功", failed: "失败", running: "运行中"};

function sideZh(v) {
  return SIDE_ZH[String(v).toLowerCase()] || v;
}

function statusZh(v) {
  return STATUS_ZH[v] || v;
}

function renderOrders(snap) {
  const table = document.getElementById("orders-table");
  const empty = document.getElementById("orders-empty");
  if (snap.orders.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    return;
  }
  empty.hidden = true;
  const list = ordersSorter ? ordersSorter.sortItems(snap.orders) : snap.orders;
  renderTable("orders-table", list.map((o) => {
    const sideTd = document.createElement("td");
    sideTd.textContent = sideZh(o.side);
    sideTd.className = o.side.toLowerCase() === "buy" ? "side-buy" : "side-sell";
    return [fmtTime(o.create_time), o.symbol, sideTd, o.status, o.qty, o.price, o.fill_qty];
  }));
}

async function loadOrders() {
  const errorEl = document.getElementById("orders-error");
  if (!errorEl) return;
  try {
    const snap = await fetchJSON("/v1/futu/orders?env=" + dashEnv);
    clearError(errorEl);
    renderOrders(snap);
  } catch (err) {
    document.getElementById("orders-table").hidden = true;
    document.getElementById("orders-empty").hidden = true;
    showError(errorEl, err);
  }
}

async function loadDashboard() {
  await Promise.all([loadEnvSnap("sim"), loadEnvSnap("real"), loadSnapSeries("sim"), loadSnapSeries("real")]);
  renderBadge();
  renderSummary();
  renderAccounts();
  renderPositions();
  loadOrders();
  if (errByEnv.sim && errByEnv.real) {
    const el = document.getElementById("dash-error");
    el.textContent = "Futu 网关不可用:模拟盘与实盘均查询失败(" + errByEnv.sim + ")。请检查网关容器状态后刷新。";
    el.hidden = false;
  }
  stampUpdated("dash-updated");
}

function renderRuns(runs) {
  renderTable("runs-table", runs.map((r) => [r.id, r.source, statusZh(r.status), r.started_at, r.finished_at === null ? "运行中" : r.finished_at]));
}

/* Dashboard 自动轮询(券商面板实时性):30s 刷新账户快照;页面隐藏时暂停
   (visibilitychange),避免后台持续打 futu 网关;手动刷新共存。 */
const AUTO_REFRESH_MS = 30000;

let autoRefreshTimer = null;
let autoRefreshFn = null;

/* 刷新按钮忙态(券商面板惯例):点击后禁用 + 「刷新中…」,完成/失败恢复;
   自动轮询路径不触发(按钮状态只跟手动点击)。 */
function wrapRefreshClick(id, busyText, fn) {
  const btn = document.getElementById(id);
  if (!btn) return;
  const idleText = btn.textContent;
  btn.addEventListener("click", () => {
    btn.disabled = true;
    btn.textContent = busyText;
    Promise.resolve(fn()).finally(() => {
      btn.disabled = false;
      btn.textContent = idleText;
    });
  });
}

function startAutoRefresh(fn) {
  if (fn) autoRefreshFn = fn;
  if (autoRefreshTimer) return;
  autoRefreshTimer = setInterval(() => {
    if (document.visibilityState === "visible") (autoRefreshFn || loadDashboard)();
  }, AUTO_REFRESH_MS);
}

function stopAutoRefresh() {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer);
  autoRefreshTimer = null;
}

function initDashboardPage() {
  const paperBtn = document.getElementById("env-paper");
  if (!paperBtn) return;
  const refresh = document.getElementById("dash-refresh");
  const buttons = [paperBtn, document.getElementById("env-real")];
  for (const btn of buttons) {
    btn.addEventListener("click", async () => {
      dashEnv = btn.id === "env-real" ? "real" : "sim";
      renderBadge();
      renderSummary();
      renderPositions();
      loadOrders();
      await loadSnapSeries(dashEnv); /* 资产曲线跟随当前 env */
      renderSummaryCurve();
    });
  }
  wrapRefreshClick("dash-refresh", "刷新中…", loadDashboard);
  positionsSorter = makeTableSorter("positions-table", POSITIONS_SORT_KEYS);
  positionsSorter.render = renderPositions;
  positionsSorter.state.key = "market_val"; /* 默认按市值降序(券商面板惯例) */
  positionsSorter.state.dir = -1;
  positionsSorter.renderIndicators();
  ordersSorter = makeTableSorter("orders-table", ORDERS_SORT_KEYS);
  ordersSorter.render = loadOrders;
  ordersSorter.state.key = "create_time"; /* 默认按时间降序,新单在上 */
  ordersSorter.state.dir = -1;
  ordersSorter.renderIndicators();
  loadDashboard();
  loadJSON("/v1/runs?limit=10", document.getElementById("runs-error"), renderRuns);
  startAutoRefresh(loadDashboard);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") stopAutoRefresh();
    else startAutoRefresh();
  });
}

/* Admin page: /v1/admin/cluster (节点状态概览), /v1/admin/config (read-only). */

/* setBadge paints a node status pill: ok (正常) / warn (部分异常) /
   down (故障) / idle (空闲), used by the cluster overview cards. */
function setBadge(id, text, cls) {
  const el = document.getElementById(id);
  el.textContent = text;
  el.className = "badge " + cls;
}

/* renderCluster paints the 4 node cards (Process / Database / Pipeline /
   Data plane) from /v1/admin/cluster; detailed lists (recent runs, coverage)
   live on other pages — this is the status overview (老板 2026-08-02 页4). */
function renderCluster(c) {
  const comps = c.components;
  setText("cluster-process-version", comps.process.version);
  setText("cluster-process-pid", comps.process.pid);
  setText("cluster-process-uptime", Math.round(comps.process.uptime_seconds) + " s");
  setText("cluster-process-listen", comps.process.listen_addr);
  setBadge("cluster-process-badge", "运行中", "ok");
  if (comps.db.ok) {
    setBadge("cluster-db-badge", "正常", "ok");
    setText("cluster-db-state", "ok");
    setText("cluster-db-latency", typeof comps.db.latency_ms === "number" ? comps.db.latency_ms + " ms" : "n/a");
  } else {
    setBadge("cluster-db-badge", "故障", "down");
    setText("cluster-db-state", "down");
    setText("cluster-db-latency", "n/a");
  }
  const counts = comps.pipeline.counts;
  setText("cluster-pipeline-running", counts.running);
  setText("cluster-pipeline-succeeded", counts.succeeded);
  setText("cluster-pipeline-failed", counts.failed);
  if (counts.failed > 0) {
    setBadge("cluster-pipeline-badge", "有失败", "warn");
  } else if (counts.running > 0) {
    setBadge("cluster-pipeline-badge", "进行中", "ok");
  } else if (counts.succeeded > 0) {
    setBadge("cluster-pipeline-badge", "正常", "ok");
  } else {
    setBadge("cluster-pipeline-badge", "空闲", "idle");
  }
  const cov = comps.data_plane.bars_coverage || [];
  const stale = cov.filter((b) => b.fresh === "stale").length;
  const unknown = cov.filter((b) => b.fresh === "unknown").length;
  setText("cluster-data-series", cov.length);
  setText("cluster-data-stale", stale + (unknown > 0 ? " (+" + unknown + " 无数据)" : ""));
  let newest = "";
  for (const b of cov) {
    if (b.max_ts && b.max_ts > newest) newest = b.max_ts;
  }
  setText("cluster-data-newest", newest === "" ? "无数据" : newest.slice(0, 16));
  if (stale > 0) {
    setBadge("cluster-data-badge", "部分过期", "warn");
  } else if (cov.length === 0) {
    setBadge("cluster-data-badge", "无数据", "idle");
  } else {
    setBadge("cluster-data-badge", "正常", "ok");
  }
  document.getElementById("cluster-cards").hidden = false;
}

/* Freshness column: stale rows are marked 数据过期, unknown rows 无数据
   (data freshness monitor, doc/DATA_PIPELINE.md). */
function freshnessCell(b) {
  const td = document.createElement("td");
  if (b.fresh === "stale") {
    td.textContent = "数据过期";
    td.classList.add("freshness-stale");
  } else if (b.fresh === "unknown") {
    td.textContent = "无数据";
    td.classList.add("freshness-unknown");
  } else {
    td.textContent = "正常";
  }
  return td;
}

function renderConfig(keys) {
  renderTable("config-table", keys.map((c) => [c.key, c.group, c.set ? "是" : "否", c.updated_at === null ? "未设置" : c.updated_at]));
}

function initAdminPage() {
  const clusterError = document.getElementById("cluster-error");
  if (!clusterError) return;
  const loadAll = () => Promise.all([
    loadJSON("/v1/admin/cluster", clusterError, renderCluster),
    loadJSON("/v1/admin/config", document.getElementById("config-error"), renderConfig),
  ]).then(() => stampUpdated("admin-updated"));
  loadAll();
  wrapRefreshClick("admin-refresh", "刷新中…", loadAll);
  startAutoRefresh(loadAll); /* cluster/config 为 PG 本地查询,30s 轮询成本低 */
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") stopAutoRefresh();
    else startAutoRefresh();
  });
}

/* Data page: cached-bars coverage (/v1/admin/cluster) with drill-in to
   /v1/bars?desc=1 (newest bars first). Covers the boss's ask for a dedicated
   "数据" tab to inspect what data is cached. */

function fmtAge(seconds) {
  if (seconds < 3600) return Math.max(1, Math.round(seconds / 60)) + " 分钟前";
  if (seconds < 86400) return Math.round(seconds / 3600) + " 小时前";
  return Math.round(seconds / 86400) + " 天前";
}

function renderCoverageRows(rows) {
  coverageRows = rows; /* 供表头排序后本地重绘 */
  const table = document.getElementById("coverage-table");
  const empty = document.getElementById("coverage-empty");
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  rows = coverageSorter ? coverageSorter.sortItems(rows) : rows;
  if (rows.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    return;
  }
  for (const b of rows) {
    const tr = document.createElement("tr");
    tr.classList.add("coverage-row");
    tr.title = "点击查看 " + b.symbol + " " + b.timeframe + " (" + b.adjust + ")";
    tr.addEventListener("click", () => loadBars(b.symbol, b.timeframe, b.adjust));
    const cells = [b.symbol, b.timeframe, b.adjust, b.count, fmtTime(b.min_ts), fmtTime(b.max_ts), fmtAge(b.max_ts_age_seconds)];
    for (const cell of cells) {
      const td = document.createElement("td");
      td.textContent = cell;
      tr.appendChild(td);
    }
    tr.appendChild(freshnessCell(b));
    const action = document.createElement("td");
    const refill = document.createElement("button");
    refill.type = "button";
    refill.className = "link";
    refill.textContent = "补数据";
    refill.title = "经网关拉取 " + b.symbol + " " + b.timeframe + "(" + b.adjust + ") 的行情,更新缓存";
    refill.addEventListener("click", (ev) => {
      ev.stopPropagation(); /* 不触发行 drill-in */
      ingestBars(b, refill);
    });
    action.appendChild(refill);
    tr.appendChild(action);
    tbody.appendChild(tr);
  }
  empty.hidden = true;
  table.hidden = false;
}

/* 期权新鲜度表:按标的×来源展示 option_quotes 最新时间与三态状态
   (与 CLI `ingest freshness` 期权区块同一阈值 4h)。无 drill-in。 */
const OPTIONS_FRESH_SORT_KEYS = {
  underlying: (o) => o.underlying,
  source: (o) => o.source,
  max_ts: (o) => o.max_ts,
};

function renderOptionsFreshness(rows) {
  optionsFreshRows = rows; /* 供表头排序后本地重绘 */
  const table = document.getElementById("options-fresh-table");
  const empty = document.getElementById("options-fresh-empty");
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  rows = optionsFreshSorter ? optionsFreshSorter.sortItems(rows) : rows;
  if (rows.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    return;
  }
  for (const o of rows) {
    const tr = document.createElement("tr");
    const cells = [o.underlying, o.source, fmtTime(o.max_ts), fmtAge(o.max_ts_age_seconds)];
    for (const cell of cells) {
      const td = document.createElement("td");
      td.textContent = cell;
      tr.appendChild(td);
    }
    tr.appendChild(freshnessCell(o));
    const action = document.createElement("td");
    const refill = document.createElement("button");
    refill.type = "button";
    refill.className = "link";
    refill.textContent = "拉取期权链";
    refill.title = "经网关拉取 " + o.underlying + " 的期权链 K 线(近端到期),更新缓存";
    refill.addEventListener("click", () => ingestOptions(o, refill));
    action.appendChild(refill);
    tr.appendChild(action);
    tbody.appendChild(tr);
  }
  empty.hidden = true;
  table.hidden = false;
}

async function loadDataCoverage() {
  const data = await fetchJSON("/v1/admin/cluster");
  renderCoverageRows(data.components.data_plane.bars_coverage || []);
  renderOptionsFreshness(data.components.data_plane.options_freshness || []);
  stampUpdated("data-updated");
}

/* 补数据:POST /v1/ingest 经网关拉取该标的行情(与 `wbot ingest futu`
   同一管线,source=http-api);成功刷新覆盖表与明细,失败显示原因
   (网关不可达/无数据等,按钮短暂置忙防重复点击)。 */
/* 拉取期权链:POST /v1/ingest {kind:"option"} 经网关拉取该标的期权链
   K 线(与 `wbot ingest futu-option` 同一管线,近端 1 个到期);成功
   后刷新覆盖表与期权表,失败显示原因(按钮短暂置忙防重复点击)。 */
async function ingestOptions(o, btn) {
  const errEl = document.getElementById("options-error");
  clearError(errEl);
  btn.disabled = true;
  btn.textContent = "拉取中…";
  try {
    await fetchJSON("/v1/ingest", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({kind: "option", symbol: o.underlying})
    });
    btn.textContent = "已更新";
    const [data] = await Promise.all([fetchJSON("/v1/admin/cluster")]);
    renderCoverageRows(data.components.data_plane.bars_coverage || []);
    renderOptionsFreshness(data.components.data_plane.options_freshness || []);
  } catch (err) {
    btn.textContent = "拉取期权链";
    showError(errEl, err);
  } finally {
    btn.disabled = false;
  }
}

async function ingestBars(b, btn) {
  const errEl = document.getElementById("coverage-error");
  clearError(errEl);
  btn.disabled = true;
  btn.textContent = "拉取中…";
  try {
    await fetchJSON("/v1/ingest", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({symbol: b.symbol, timeframe: b.timeframe, adjust: b.adjust})
    });
    btn.textContent = "已更新";
    const [data] = await Promise.all([fetchJSON("/v1/admin/cluster")]);
    renderCoverageRows(data.components.data_plane.bars_coverage || []);
    if (document.getElementById("detail").hidden === false && document.getElementById("detail-title").textContent.includes(b.symbol)) {
      loadBars(b.symbol, b.timeframe, b.adjust); /* 明细视图同步刷新 */
    }
  } catch (err) {
    btn.textContent = "补数据";
    showError(errEl, err);
  } finally {
    btn.disabled = false;
  }
}

function fmtNum(v) {
  return Number(v).toLocaleString("en-US", { maximumFractionDigits: 2 });
}

/* 图表重绘缓存 (UI 主题化): 记录每张 canvas 的最后绘制参数, 主题切换时
   立即重绘, 无需刷新页面 (initTheme 的 apply 后调用 redrawCharts). */
const chartCache = new Map();

function redrawCharts() {
  for (const [canvas, draw] of chartCache) {
    if (canvas.isConnected) draw();
  }
}

function drawSparkline(canvas, closes) {
  const draw = () => {
    const width = canvas.clientWidth || 600;
    const height = canvas.clientHeight || 120;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    const ctx = canvas.getContext("2d");
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, width, height);
    if (closes.length < 2) return;
    let min = Infinity, max = -Infinity;
    for (const c of closes) { min = Math.min(min, c); max = Math.max(max, c); }
    const span = max - min || 1;
    const pad = 4;
    ctx.strokeStyle = closes[closes.length - 1] >= closes[0] ? (cssVar("--ok") || "#1a7f37") : (cssVar("--down") || "#cf222e");
    ctx.lineWidth = 2;
    ctx.beginPath();
    for (let i = 0; i < closes.length; i++) {
      const x = pad + (i / (closes.length - 1)) * (width - 2 * pad);
      const y = pad + (1 - (closes[i] - min) / span) * (height - 2 * pad);
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    }
    ctx.stroke();
  };
  chartCache.set(canvas, draw);
}

/* 行情明细周期 tab:富途 K 线交互参照——点 tab 即切周期重载,
   无需回顶部表单。模块级记忆当前 symbol/adjust(loadBars 记录)。 */
const DETAIL_TIMEFRAMES = ["1m", "5m", "15m", "30m", "60m", "1d", "1w", "1mo"];
let detailSymbol = "";
let detailAdjust = "fwd";

function renderDetailTabs(timeframe) {
  const wrap = document.getElementById("detail-tabs");
  wrap.replaceChildren();
  for (const tf of DETAIL_TIMEFRAMES) {
    const b = document.createElement("button");
    b.type = "button";
    b.textContent = tf;
    b.classList.toggle("active", tf === timeframe);
    b.addEventListener("click", () => {
      if (detailSymbol !== "") loadBars(detailSymbol, tf, detailAdjust);
    });
    wrap.appendChild(b);
  }
}

function renderBarsDetail(bars) {
  const table = document.getElementById("bars-table");
  const empty = document.getElementById("bars-empty");
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  if (bars.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    return;
  }
  /* &desc=1: bars[0] is the newest, bars[len-1] the oldest; stats and the
     sparkline read chronologically from oldest to newest. */
  const first = bars[bars.length - 1];
  const last = bars[0];
  setText("detail-count", bars.length);
  setText("detail-first", fmtNum(first.close));
  setText("detail-last", fmtNum(last.close));
  const chg = (last.close - first.close) / first.close;
  const chgEl = document.getElementById("detail-change");
  chgEl.textContent = (chg >= 0 ? "+" : "") + (chg * 100).toFixed(2) + "%";
  chgEl.classList.toggle("up", chg >= 0);
  chgEl.classList.toggle("down", chg < 0);
  drawSparkline(document.getElementById("detail-sparkline"), bars.map((b) => b.close));
  for (let i = 0; i < bars.length; i++) {
    const b = bars[i];
    const row = document.createElement("tr");
    const cells = [b.ts.slice(0, 16).replace("T", " "), fmtNum(b.open), fmtNum(b.high), fmtNum(b.low), fmtNum(b.close)];
    for (const cell of cells) {
      const td = document.createElement("td");
      td.textContent = cell;
      row.appendChild(td);
    }
    /* 涨跌幅:相对前一根(desc 序下数组后一项,即更早一根)。 */
    const prev = bars[i + 1];
    const pctEl = document.createElement("td");
    if (prev) {
      const pct = (b.close - prev.close) / prev.close;
      pctEl.textContent = (pct >= 0 ? "+" : "") + (pct * 100).toFixed(2) + "%";
      pctEl.classList.toggle("up", pct >= 0);
      pctEl.classList.toggle("down", pct < 0);
    } else {
      pctEl.textContent = "—";
      pctEl.classList.add("muted");
    }
    row.appendChild(pctEl);
    const vol = document.createElement("td");
    vol.textContent = b.volume;
    row.appendChild(vol);
    tbody.appendChild(row);
  }
  empty.hidden = true;
  table.hidden = false;
}

async function loadBars(symbol, timeframe, adjust) {
  const errEl = document.getElementById("detail-error");
  clearError(errEl);
  const empty = document.getElementById("detail-empty");
  empty.hidden = true;
  detailSymbol = symbol;
  detailAdjust = adjust;
  renderDetailTabs(timeframe);
  document.getElementById("detail-body").hidden = false;
  setText("detail-title", symbol + " · " + timeframe + " · " + adjust);
  document.getElementById("bars-symbol").value = symbol;
  document.getElementById("bars-timeframe").value = timeframe;
  document.getElementById("bars-adjust").value = adjust;
  try {
    const bars = await fetchJSON("/v1/bars?symbol=" + encodeURIComponent(symbol) + "&timeframe=" + encodeURIComponent(timeframe) + "&adjust=" + encodeURIComponent(adjust) + "&limit=100&desc=1");
    renderBarsDetail(bars);
  } catch (err) {
    showError(errEl, err);
    document.getElementById("bars-table").hidden = true;
    document.getElementById("bars-empty").hidden = false;
    document.getElementById("bars-empty").textContent = "加载失败,请检查代码与周期。";
  }
}

function initDataPage() {
  const form = document.getElementById("bars-form");
  if (!form) return;
  form.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const symbol = document.getElementById("bars-symbol").value.trim();
    const timeframe = document.getElementById("bars-timeframe").value;
    const adjust = document.getElementById("bars-adjust").value;
    if (symbol !== "") loadBars(symbol, timeframe, adjust);
  });
  wrapRefreshClick("data-refresh", "刷新中…", () => {
    const errEl = document.getElementById("data-error");
    clearError(errEl);
    return loadDataCoverage().catch((err) => showError(errEl, err));
  });
  coverageSorter = makeTableSorter("coverage-table", COVERAGE_SORT_KEYS);
  coverageSorter.render = () => renderCoverageRows(coverageRows);
  coverageSorter.state.key = "max_ts"; /* 默认按最新 bar 降序,新数据在前 */
  coverageSorter.state.dir = -1;
  coverageSorter.renderIndicators();
  optionsFreshSorter = makeTableSorter("options-fresh-table", OPTIONS_FRESH_SORT_KEYS);
  optionsFreshSorter.render = () => renderOptionsFreshness(optionsFreshRows);
  optionsFreshSorter.state.key = "max_ts"; /* 同 coverage:最新在前 */
  optionsFreshSorter.state.dir = -1;
  optionsFreshSorter.renderIndicators();
  loadDataCoverage().catch((err) => showError(document.getElementById("coverage-error"), err));
  /* coverage 为 PG 本地查询,30s 轮询成本低(与 Admin 一致);轮询路径静默
     吞错(瞬时失败下一 tick 重试),首载/手动刷新仍显示错误。 */
  startAutoRefresh(() => loadDataCoverage().catch(() => {}));
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") stopAutoRefresh();
    else startAutoRefresh();
  });
}

/* Watchlist page: /v1/watchlist CRUD + /v1/strategies schema-driven param form. */

/* fieldsetId defaults to the watchlist page's #param-fields; the 回测页 run
   form passes "run-param-fields" to render its own param inputs. */
function renderParamFields(strategy, values, fieldsetId) {
  const fields = document.getElementById(fieldsetId || "param-fields");
  fields.replaceChildren();
  const legend = document.createElement("legend");
  legend.textContent = "参数";
  fields.appendChild(legend);
  if (!strategy) return;
  for (const p of strategy.params) {
    const label = document.createElement("label");
    const name = document.createElement("span");
    name.textContent = p.name + (p.description ? " · " + p.description : "");
    label.appendChild(name);
    let value = values && values[p.name] !== undefined ? values[p.name] : p.default;
    let input;
    if (p.type === "choice") {
      input = document.createElement("select");
      for (const choice of p.choices) {
        const opt = document.createElement("option");
        opt.value = choice;
        opt.textContent = choice;
        input.appendChild(opt);
      }
      if (p.choices.indexOf(value) === -1) value = p.choices[0];
    } else if (p.type === "number") {
      input = document.createElement("input");
      input.type = "number";
      input.step = "any";
    } else {
      input = document.createElement("input");
      input.type = "text";
    }
    input.name = "params." + p.name;
    input.value = value === undefined || value === null ? "" : value;
    label.appendChild(input);
    fields.appendChild(label);
  }
}

function collectParams(strategy, form) {
  const params = {};
  for (const p of strategy.params) {
    const raw = form.elements["params." + p.name].value;
    if (raw === "") continue; /* omit: strategy default applies */
    if (p.type === "number") {
      const n = Number(raw);
      if (!isFinite(n)) return {error: "invalid number for " + p.name};
      params[p.name] = n;
    } else if (p.type === "choice") {
      if (p.choices.indexOf(raw) === -1) return {error: "invalid choice for " + p.name};
      params[p.name] = raw;
    } else {
      params[p.name] = raw;
    }
  }
  return {params: params};
}

function renderWatchlist(items, onEdit, onDelete, onBacktest) {
  const table = document.getElementById("watchlist-table");
  const empty = document.getElementById("watchlist-empty");
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  if (items.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    return;
  }
  for (const item of items) {
    const tr = document.createElement("tr");
    for (const cell of [item.symbol, item.strategy, JSON.stringify(item.params), item.updated_at]) {
      const td = document.createElement("td");
      td.textContent = cell;
      tr.appendChild(td);
    }
    const actions = document.createElement("td");
    const edit = document.createElement("button");
    edit.type = "button";
    edit.className = "link";
    edit.textContent = "编辑";
    edit.addEventListener("click", () => onEdit(item));
    const run = document.createElement("button");
    run.type = "button";
    run.className = "link";
    run.textContent = "回测";
    run.addEventListener("click", () => onBacktest(item));
    const del = document.createElement("button");
    del.type = "button";
    del.className = "link danger";
    del.textContent = "删除";
    del.addEventListener("click", () => onDelete(item));
    actions.appendChild(edit);
    actions.appendChild(run);
    actions.appendChild(del);
    tr.appendChild(actions);
    tbody.appendChild(tr);
  }
  empty.hidden = true;
  table.hidden = false;
}

function initWatchlistPage() {
  const form = document.getElementById("watchlist-form");
  if (!form) return;
  const strategySelect = document.getElementById("strategy-select");
  const formError = document.getElementById("watchlist-form-error");
  const formOk = document.getElementById("watchlist-form-ok");
  const listError = document.getElementById("watchlist-error");
  let strategies = [];
  let editingSymbol = null;

  function hideOk() {
    formOk.hidden = true;
  }

  function strategyByName(name) {
    for (const s of strategies) {
      if (s.name === name) return s;
    }
    return null;
  }

  function currentStrategy() {
    return strategyByName(strategySelect.value);
  }

  function renderStrategySelect() {
    strategySelect.replaceChildren();
    for (const s of strategies) {
      const opt = document.createElement("option");
      opt.value = s.name;
      opt.textContent = s.name;
      strategySelect.appendChild(opt);
    }
    renderParamFields(currentStrategy());
  }

  function loadStrategies() {
    loadJSON("/v1/strategies", formError, (list) => {
      strategies = list;
      renderStrategySelect();
      renderStrategyCards(list);
      const cards = document.querySelectorAll(".strategy-card");
      for (const card of cards) {
        card.addEventListener("click", () => {
          strategySelect.value = card.dataset.strategy;
          renderParamFields(currentStrategy());
          clearError(formError);
          hideOk();
          document.getElementById("editor").scrollIntoView();
        });
      }
    });
  }

  /* 一键回测:POST /v1/backtests(该标的绑定的策略与参数),成功后带
     hash 跳到 results 页打开详情(配置→回测→看结果闭环)。 */
  function runBacktest(item) {
    const body = {symbol: item.symbol, strategy: item.strategy};
    if (item.params) body.params = item.params;
    fetchJSON("/v1/backtests", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(body),
    }).then((res) => {
      location.href = "/ui/results.html#bt-" + res.id;
    }).catch((err) => {
      showError(formError, err.message);
    });
  }

  function loadWatchlist() {
    loadJSON("/v1/watchlist", listError, (items) => {
      watchlistItems = items;
      renderWatchlist(watchlistSorter.sortItems(items), beginEdit, deleteItem, runBacktest);
    });
  }

  /* 观察列表排序(全站最后一列无排序的表):默认按更新时间降序,新更新在上。 */
  const watchlistSorter = makeTableSorter("watchlist-table", WATCHLIST_SORT_KEYS);
  watchlistSorter.render = () => renderWatchlist(watchlistItems, beginEdit, deleteItem, runBacktest);
  watchlistSorter.state.key = "updated_at";
  watchlistSorter.state.dir = -1;
  watchlistSorter.renderIndicators();

  function beginEdit(item) {
    editingSymbol = item.symbol;
    form.symbol.value = item.symbol;
    strategySelect.value = item.strategy;
    renderParamFields(currentStrategy(), item.params);
    clearError(formError);
    hideOk();
    document.getElementById("editor").scrollIntoView();
  }

  function resetForm() {
    editingSymbol = null;
    form.symbol.value = "";
    renderParamFields(currentStrategy());
    form.symbol.focus();
  }

  async function deleteItem(item) {
    if (!confirm("从观察列表移除 " + item.symbol + "?")) return;
    try {
      await fetchJSON("/v1/watchlist/" + encodeURIComponent(item.symbol), {method: "DELETE"});
      loadWatchlist();
    } catch (err) {
      showError(listError, err);
    }
  }

  strategySelect.addEventListener("change", () => {
    renderParamFields(currentStrategy());
    hideOk();
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const symbol = form.symbol.value.trim();
    if (!symbol) {
      showError(formError, new Error("symbol is required"));
      return;
    }
    const strategy = currentStrategy();
    if (!strategy) {
      showError(formError, new Error("请选择策略"));
      return;
    }
    const collected = collectParams(strategy, form);
    if (collected.error) {
      showError(formError, new Error(collected.error));
      return;
    }
    try {
      await fetchJSON("/v1/watchlist/" + encodeURIComponent(symbol), {
        method: "PUT",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({strategy: strategy.name, params: collected.params})
      });
      clearError(formError);
      formOk.textContent = "已保存 " + symbol + "(" + strategy.name + ")。";
      formOk.hidden = false;
      resetForm();
      loadWatchlist();
    } catch (err) {
      hideOk();
      showError(formError, err);
    }
  });

  loadStrategies();
  loadWatchlist();
}

/* Results page: /v1/backtests list + detail with a hand-drawn equity curve,
   plus the 启动回测 form (POST /v1/backtests, synchronous). */

/* Run form: /v1/strategies schema → strategy select + param fields, then
   POST /v1/backtests. Single run returns the new result detail (open it);
   from_watchlist returns {runs: [...]} (refresh the list). */
function setupBacktestRunForm() {
  const form = document.getElementById("backtest-form");
  if (!form) return;
  const select = document.getElementById("run-strategy-select");
  const watchlistCheck = document.getElementById("run-watchlist");
  const symbolInput = document.getElementById("run-symbol");
  const btn = document.getElementById("run-btn");
  const errEl = document.getElementById("run-error");
  const okEl = document.getElementById("run-ok");
  const listError = document.getElementById("results-error");
  let strategies = [];

  function strategyByName(name) {
    for (const s of strategies) {
      if (s.name === name) return s;
    }
    return null;
  }

  function renderSelect() {
    select.replaceChildren();
    for (const s of strategies) {
      const opt = document.createElement("option");
      opt.value = s.name;
      opt.textContent = s.name;
      select.appendChild(opt);
    }
    renderParamFields(strategyByName(select.value), null, "run-param-fields");
  }

  function refreshList() {
    loadJSON("/v1/backtests?limit=50", listError, (items) => {
      renderResultsList(items, (item) => openDetail(item.id));
    });
  }

  select.addEventListener("change", () => {
    renderParamFields(strategyByName(select.value), null, "run-param-fields");
    clearError(errEl);
  });

  watchlistCheck.addEventListener("change", () => {
    symbolInput.disabled = watchlistCheck.checked;
    clearError(errEl);
  });

  form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    clearError(errEl);
    okEl.hidden = true;
    const strategy = strategyByName(select.value);
    if (!strategy) {
      showError(errEl, new Error("请选择策略"));
      return;
    }
    let body;
    if (watchlistCheck.checked) {
      body = {from_watchlist: true};
    } else {
      const symbol = symbolInput.value.trim();
      if (!symbol) {
        showError(errEl, new Error("symbol is required (或勾选使用观察列表全部标的)"));
        return;
      }
      const collected = collectParams(strategy, form);
      if (collected.error) {
        showError(errEl, new Error(collected.error));
        return;
      }
      body = {symbol: symbol, strategy: strategy.name, params: collected.params};
    }
    btn.disabled = true;
    btn.textContent = "运行中…";
    try {
      const res = await fetchJSON("/v1/backtests", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify(body)
      });
      refreshList();
      if (res.runs) {
        okEl.hidden = false;
        okEl.textContent = "完成:" + res.runs.length + " 条回测已保存,见下方列表。";
        document.getElementById("list").scrollIntoView();
      } else if (res.id) {
        okEl.hidden = false;
        okEl.textContent = "回测 #" + res.id + " 完成,已打开详情。";
        openDetail(res.id);
      }
    } catch (err) {
      showError(errEl, err);
    } finally {
      btn.disabled = false;
      btn.textContent = "运行回测";
    }
  });

  loadJSON("/v1/strategies", errEl, (list) => {
    strategies = list;
    renderSelect();
  });

  /* 详情「重新运行」:把该回测的代码/策略/参数填回顶部表单(参数
     renderParamFields 回填,与 watchlist 编辑同款),滚到表单微调即
     可重跑——看结果→调参→再跑 的迭代闭环。 */
  rerunHandler = (d) => {
    document.getElementById("rerun-btn").onclick = () => {
      const symbolInput = document.getElementById("run-symbol");
      document.getElementById("run-watchlist").checked = false;
      symbolInput.disabled = false;
      symbolInput.value = d.symbol;
      const st = strategyByName(d.strategy);
      if (st) {
        select.value = d.strategy;
        renderParamFields(st, d.params, "run-param-fields");
        clearError(errEl);
      } else {
        /* 内部基准策略(如 buy-hold)不在注册表,select 无法回填:
           保留代码,提示手动选策略。 */
        showError(errEl, new Error("策略「" + d.strategy + "」不在当前注册表,请手动选择策略。"));
      }
      document.getElementById("run").scrollIntoView();
      symbolInput.focus();
    };
  };
}

const CURVE_PAD = {top: 12, right: 64, bottom: 26, left: 48};

function cssVar(name) {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name);
  return raw ? raw.trim() : null;
}

/* palette from style.css vars, hex fallbacks; resolved at draw time so a
   theme switch repaints correctly (see redrawCharts). */
const CURVE_LINE = () => cssVar("--accent") || "#0969da";
const CURVE_ALT = () => cssVar("--accent-2") || "#eb6834";
const CURVE_GRID = () => cssVar("--border") || "#d0d7de";
const CURVE_TEXT = () => cssVar("--muted") || "#656d76";

function fmtMoney(v) {
  return Number(v).toLocaleString("en-US", {maximumFractionDigits: 0});
}

function fmtPct(v) {
  return (Number(v) * 100).toFixed(2) + "%";
}

function fmtMetric(v, formatter) {
  return v === null || v === undefined ? "—" : formatter(v);
}

function metricOf(item, key) {
  const m = item.metrics;
  return m && m[key] !== undefined ? m[key] : null;
}

/* Compare selection: exactly two runs (checkbox column + compare button). */
let compareSelection = [];

function toggleCompareSelection(id, checked) {
  const btn = document.getElementById("compare-btn");
  const hint = document.getElementById("compare-hint");
  if (checked && compareSelection.length >= 2) {
    hint.textContent = "请选择恰好两条回测进行对比。";
    hint.hidden = false;
    return false;
  }
  if (checked) {
    compareSelection.push(id);
  } else {
    compareSelection = compareSelection.filter((x) => x !== id);
  }
  hint.hidden = true;
  btn.disabled = compareSelection.length !== 2;
  return true;
}

function renderResultsList(items, onOpen) {
  const table = document.getElementById("results-table");
  const empty = document.getElementById("results-empty");
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  if (items.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    return;
  }
  for (const item of items) {
    const tr = document.createElement("tr");
    tr.dataset.id = String(item.id);
    const pick = document.createElement("td");
    const check = document.createElement("input");
    check.type = "checkbox";
    check.className = "row-check";
    check.checked = compareSelection.indexOf(item.id) !== -1;
    check.setAttribute("aria-label", "select run " + item.id + " for comparison");
    check.addEventListener("change", () => {
      if (!toggleCompareSelection(item.id, check.checked)) check.checked = false;
    });
    pick.appendChild(check);
    tr.appendChild(pick);
    for (const cell of [
      item.id,
      item.strategy,
      item.symbol,
      fmtMetric(metricOf(item, "equity"), fmtMoney)
    ]) {
      const td = document.createElement("td");
      td.textContent = cell;
      tr.appendChild(td);
    }
    tr.appendChild(numCell(metricOf(item, "total_return"), fmtPct));
    for (const cell of [
      fmtMetric(metricOf(item, "max_drawdown"), fmtPct),
      fmtMetric(metricOf(item, "bars"), String),
      fmtTime(item.created_at)
    ]) {
      const td = document.createElement("td");
      td.textContent = cell;
      tr.appendChild(td);
    }
    const actions = document.createElement("td");
    const open = document.createElement("button");
    open.type = "button";
    open.className = "link";
    open.textContent = "详情";
    open.addEventListener("click", () => onOpen(item));
    actions.appendChild(open);
    tr.appendChild(actions);
    tbody.appendChild(tr);
  }
  empty.hidden = true;
  table.hidden = false;
  /* 排序重绘后恢复详情选中高亮(selectResultsRow 按 dataset.id 匹配)。 */
  if (openDetailId !== null) selectResultsRow(openDetailId);
}

/* 通用表头排序(券商面板惯例):makeTableSorter 绑定表格表头 data-sort
   键与取值器,点击切换升/降序, ↕/↑/↓ 指示当前排序;sortItems 供渲染
   前调用(数字列按值、字符串列按字典序,缺失值按 -Infinity 沉底)。
   回测结果表 / 持仓表复用。 */
function makeTableSorter(tableId, getters) {
  const ths = document.querySelectorAll("#" + tableId + " th[data-sort]");
  const state = {key: null, dir: 1}; /* dir: 1 升序, -1 降序 */
  const sorter = {state: state, sortItems: null, render: null};
  sorter.sortItems = (items) => {
    if (!state.key || !getters[state.key]) return items;
    const get = getters[state.key];
    return items.slice().sort((a, b) => {
      const va = get(a);
      const vb = get(b);
      if (va === vb) return 0;
      if (typeof va === "string") return state.dir * String(va).localeCompare(String(vb));
      return state.dir * (va - vb);
    });
  };
  const renderIndicators = () => {
    for (const th of ths) {
      const base = th.dataset.label || th.textContent.replace(/[↑↓↕]/g, "").trim();
      th.dataset.label = base;
      th.textContent = base + (th.dataset.sort === state.key
        ? (state.dir === 1 ? " ↑" : " ↓")
        : " ↕");
    }
  };
  for (const th of ths) {
    th.addEventListener("click", () => {
      const key = th.dataset.sort;
      if (state.key === key) {
        state.dir = -state.dir;
      } else {
        state.key = key;
        state.dir = 1;
      }
      renderIndicators();
      if (sorter.render) sorter.render();
    });
  }
  sorter.renderIndicators = renderIndicators; /* 暴露:init 默认排序时同步表头指示 */
  return sorter;
}

const RESULTS_SORT_KEYS = {
  id: (i) => i.id,
  strategy: (i) => i.strategy,
  symbol: (i) => i.symbol,
  equity: (i) => metricOf(i, "equity") ?? -Infinity,
  total_return: (i) => metricOf(i, "total_return") ?? -Infinity,
  max_drawdown: (i) => metricOf(i, "max_drawdown") ?? -Infinity,
  bars: (i) => metricOf(i, "bars") ?? -Infinity,
  created_at: (i) => i.created_at,
};

/* 持仓表排序:市值/盈亏按值比较(盈亏缺省按 -Infinity 沉底,富途面板
   持仓表默认按市值排)。 */
const POSITIONS_SORT_KEYS = {
  symbol: (p) => p.symbol,
  qty: (p) => p.qty ?? -Infinity,
  avg_cost: (p) => p.avg_cost ?? -Infinity,
  price: (p) => p.price ?? -Infinity,
  market_val: (p) => p.market_val ?? -Infinity,
  pl: (p) => p.pl ?? -Infinity,
};

/* 订单表排序:数量/价格/已成交按值比较(缺失沉底)。 */
const ORDERS_SORT_KEYS = {
  create_time: (o) => o.create_time,
  symbol: (o) => o.symbol,
  side: (o) => o.side,
  status: (o) => o.status,
  qty: (o) => o.qty ?? -Infinity,
  price: (o) => o.price ?? -Infinity,
  fill_qty: (o) => o.fill_qty ?? -Infinity,
};

/* Data 页覆盖表排序:数量按值,日期为定长字符串字典序=时间序。 */
let optionsFreshRows = []; /* 期权新鲜度行缓存(排序重绘用) */
let optionsFreshSorter = null;

const COVERAGE_SORT_KEYS = {
  symbol: (b) => b.symbol,
  timeframe: (b) => b.timeframe,
  count: (b) => b.count ?? -Infinity,
  min_ts: (b) => b.min_ts,
  max_ts: (b) => b.max_ts,
};

/* 观察列表排序:更新时间定长字符串字典序=时间序。 */
const WATCHLIST_SORT_KEYS = {
  symbol: (i) => i.symbol,
  strategy: (i) => i.strategy,
  updated_at: (i) => i.updated_at,
};

let positionsSorter = null;
let ordersSorter = null;
let watchlistItems = []; /* 最近一次 /v1/watchlist 结果,排序 render 闭包引用 */
let coverageSorter = null;
let coverageRows = []; /* 最近一次覆盖表数据:sorter.render 本地重绘用 */

function showMetric(id, v, formatter) {
  setText(id, fmtMetric(v, formatter));
}

/* 长回测 trades 可能上千条:默认只渲染最近 TRADES_LIMIT 条,避免 DOM 爆炸
   页面过长;超限时显示提示 + 「显示全部」展开(点击后全量重绘)。 */
const TRADES_LIMIT = 100;

function renderTradesTable(trades) {
  const table = document.getElementById("trades-table");
  const empty = document.getElementById("trades-empty");
  const hint = document.getElementById("trades-limit-hint");
  const showAll = document.getElementById("trades-show-all");
  const rows = trades.map((t) => {
    const actionTd = document.createElement("td");
    actionTd.textContent = sideZh(t.action);
    actionTd.className = String(t.action).toLowerCase() === "buy" ? "side-buy" : "side-sell";
    return [fmtTime(t.ts), actionTd, t.symbol, t.size, t.price, t.cash_after];
  });
  if (rows.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    hint.hidden = true;
    showAll.hidden = true;
    return;
  }
  const limited = rows.length > TRADES_LIMIT;
  renderTable("trades-table", limited ? rows.slice(-TRADES_LIMIT) : rows);
  hint.hidden = !limited;
  if (limited) {
    hint.textContent = "共 " + rows.length + " 笔交易,仅显示最近 " + TRADES_LIMIT + " 笔。";
  }
  showAll.hidden = !limited;
  showAll.onclick = () => {
    renderTable("trades-table", rows);
    hint.hidden = true;
    showAll.hidden = true;
  };
}

function renderDetail(d) {
  setText("detail-id", d.id);
  showMetric("metric-equity", metricOf(d, "equity"), fmtMoney);
  showMetric("metric-total-return", metricOf(d, "total_return"), fmtPct);
  showMetric("metric-max-drawdown", metricOf(d, "max_drawdown"), fmtPct);
  showMetric("metric-bars", metricOf(d, "bars"), String);
  document.getElementById("metric-cards").hidden = false;
  document.getElementById("curve-wrap").hidden = false;
  document.getElementById("detail-extra").hidden = false;
  wireExport(d.id);
  if (rerunHandler) rerunHandler(d); /* initResultsPage 注入,重新运行表单回填 */
  renderTradesTable(d.trades || []);
  document.getElementById("detail-params").textContent = d.params ? JSON.stringify(d.params, null, 2) : "{}";
  curvePoints = d.equity_curve || [];
  curveIndex = 0;
  renderCurve(null);
  const detail = document.getElementById("detail");
  detail.hidden = false;
  detail.scrollIntoView();
}

function showDetailError(err) {
  showError(document.getElementById("detail-error"), err);
  document.getElementById("metric-cards").hidden = true;
  document.getElementById("curve-wrap").hidden = true;
  document.getElementById("detail-extra").hidden = true;
  const detail = document.getElementById("detail");
  detail.hidden = false;
  detail.scrollIntoView();
}

/* 导出:浏览器直接下载服务端序列化(与 `wbot backtest -export` 同一
   serializer,CSV/JSON 契约一致)。点击后 attachment 下载,页面不跳转。 */
function wireExport(id) {
  document.getElementById("export-csv").onclick = () => {
    location.href = "/v1/backtests/" + id + "/export?format=csv";
  };
  document.getElementById("export-json").onclick = () => {
    location.href = "/v1/backtests/" + id + "/export?format=json";
  };
}

/* 详情视图高亮列表中当前查看的行(选中态),返回列表时一眼定位。 */
function selectResultsRow(id) {
  const tbody = document.querySelector("#results-table tbody");
  if (!tbody) return;
  for (const tr of tbody.rows) {
    tr.classList.toggle("selected", tr.dataset.id === String(id));
  }
}

let openDetailId = null;
let rerunHandler = null; /* 由 initResultsPage 注入:详情「重新运行」回填表单 */

function openDetail(id) {
  openDetailId = id;
  selectResultsRow(id);
  fetchJSON("/v1/backtests/" + id).then((d) => {
    clearError(document.getElementById("detail-error"));
    renderDetail(d);
  }).catch(showDetailError);
}

function niceStep(range) {
  const raw = range / 4;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  for (const m of [1, 2, 5, 10]) {
    if (raw <= m * mag) return m * mag;
  }
  return 10 * mag;
}

function fmtAxis(v) {
  const abs = Math.abs(v);
  if (abs >= 1000000) return (v / 1000000).toFixed(1) + "M";
  if (abs >= 1000) return (v / 1000).toFixed(1) + "k";
  return String(Math.round(v));
}

/* drawCurvePlot renders one or more equity series on a canvas: shared
   time-domain x scale, per-series color, final-point label per series. */
function drawCurvePlot(canvas, series, hover) {
  const ctx = canvas.getContext("2d");
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  const w = canvas.width - CURVE_PAD.left - CURVE_PAD.right;
  const h = canvas.height - CURVE_PAD.top - CURVE_PAD.bottom;
  let min = Infinity;
  let max = -Infinity;
  let tsMin = Infinity;
  let tsMax = -Infinity;
  for (const s of series) {
    for (const p of s.points) {
      min = Math.min(min, p.equity);
      max = Math.max(max, p.equity);
      const t = Date.parse(p.ts);
      tsMin = Math.min(tsMin, t);
      tsMax = Math.max(tsMax, t);
    }
  }
  if (min === max) {
    min -= 1;
    max += 1;
  }
  const step = niceStep(max - min);
  const lo = Math.floor(min / step) * step;
  const hi = Math.ceil(max / step) * step;
  const span = tsMax - tsMin;
  const x = (ts) => (span === 0 ? CURVE_PAD.left + w / 2 : CURVE_PAD.left + ((ts - tsMin) / span) * w);
  const y = (v) => CURVE_PAD.top + (1 - (v - lo) / (hi - lo)) * h;
  ctx.lineWidth = 1;
  ctx.strokeStyle = CURVE_GRID();
  ctx.fillStyle = CURVE_TEXT();
  ctx.font = "10px system-ui, sans-serif";
  ctx.textAlign = "right";
  ctx.textBaseline = "middle";
  for (let v = lo; v <= hi + step / 2; v += step) {
    ctx.beginPath();
    ctx.moveTo(CURVE_PAD.left, y(v));
    ctx.lineTo(canvas.width - CURVE_PAD.right, y(v));
    ctx.stroke();
    ctx.fillText(fmtAxis(v), CURVE_PAD.left - 6, y(v));
  }
  const labelSeries = series.find((s) => s.points.length > 0);
  if (labelSeries) {
    const first = labelSeries.points;
    ctx.textAlign = "center";
    ctx.textBaseline = "alphabetic";
    for (const i of [0, Math.floor((first.length - 1) / 2), first.length - 1]) {
      ctx.fillText(String(first[i].ts).slice(0, 10), x(Date.parse(first[i].ts)), canvas.height - 8);
    }
  }
  ctx.lineWidth = 2;
  ctx.lineJoin = "round";
  ctx.lineCap = "round";
  for (const s of series) {
    const pts = s.points;
    if (pts.length === 0) continue;
    ctx.strokeStyle = s.color;
    ctx.beginPath();
    ctx.moveTo(x(Date.parse(pts[0].ts)), y(pts[0].equity));
    for (let i = 1; i < pts.length; i++) {
      ctx.lineTo(x(Date.parse(pts[i].ts)), y(pts[i].equity));
    }
    ctx.stroke();
    const last = pts.length - 1;
    ctx.beginPath();
    ctx.arc(x(Date.parse(pts[last].ts)), y(pts[last].equity), 4, 0, 2 * Math.PI);
    ctx.fillStyle = s.color;
    ctx.fill();
    ctx.fillStyle = CURVE_TEXT();
    ctx.textAlign = "left";
    ctx.textBaseline = "middle";
    ctx.fillText(fmtMoney(pts[last].equity), x(Date.parse(pts[last].ts)) + 10, y(pts[last].equity));
  }
  if (hover === null || hover === undefined) return;
  const hp = series[0].points[hover];
  ctx.lineWidth = 1;
  ctx.strokeStyle = CURVE_GRID();
  ctx.beginPath();
  ctx.moveTo(x(Date.parse(hp.ts)), CURVE_PAD.top);
  ctx.lineTo(x(Date.parse(hp.ts)), CURVE_PAD.top + h);
  ctx.stroke();
  ctx.beginPath();
  ctx.arc(x(Date.parse(hp.ts)), y(hp.equity), 4, 0, 2 * Math.PI);
  ctx.fillStyle = CURVE_LINE();
  ctx.fill();
  ctx.strokeStyle = "#fff";
  ctx.lineWidth = 2;
  ctx.stroke();
}

function drawEquityCurve(points, hoverIdx) {
  const canvas = document.getElementById("equity-canvas");
  const empty = document.getElementById("curve-empty");
  if (!points || points.length === 0) {
    canvas.hidden = true;
    empty.hidden = false;
    return;
  }
  canvas.hidden = false;
  empty.hidden = true;
  const draw = () => drawCurvePlot(canvas, [{points: points, color: CURVE_LINE()}], hoverIdx);
  chartCache.set(canvas, draw);
  draw();
}

function drawMultiCurve(series, canvasId) {
  const canvas = document.getElementById(canvasId);
  const empty = document.getElementById("compare-empty");
  if (!series.some((s) => s.points && s.points.length > 0)) {
    canvas.hidden = true;
    empty.hidden = false;
    return;
  }
  canvas.hidden = false;
  empty.hidden = true;
  const draw = () => drawCurvePlot(canvas, series, null);
  chartCache.set(canvas, draw);
  draw();
}

/* Compare view: side-by-side metrics + overlaid equity curves with legend. */

function runLabel(r) {
  return "#" + r.id + " " + r.strategy + " " + r.symbol;
}

const COMPARE_METRICS = [
  ["equity", "期末权益", fmtMoney],
  ["total_return", "总收益率", fmtPct],
  ["max_drawdown", "最大回撤", fmtPct],
  ["bars", "K 线数", String]
];

function renderCompareLegend(runs, colors) {
  const legend = document.getElementById("compare-legend");
  legend.replaceChildren();
  for (let i = 0; i < runs.length; i++) {
    const item = document.createElement("span");
    item.className = "legend-item";
    const swatch = document.createElement("span");
    swatch.className = "legend-swatch";
    swatch.style.background = colors[i];
    const text = document.createElement("span");
    text.textContent = runLabel(runs[i]);
    item.appendChild(swatch);
    item.appendChild(text);
    legend.appendChild(item);
  }
}

function renderCompare(runs) {
  const compare = document.getElementById("compare");
  compare.hidden = false;
  compare.scrollIntoView();
  const colors = [CURVE_LINE(), CURVE_ALT()];
  const table = document.getElementById("compare-table");
  const headRow = table.tHead.rows[0];
  headRow.replaceChildren();
  const metricHead = document.createElement("th");
  metricHead.textContent = "指标";
  headRow.appendChild(metricHead);
  for (let i = 0; i < runs.length; i++) {
    const th = document.createElement("th");
    th.textContent = runLabel(runs[i]);
    headRow.appendChild(th);
  }
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  for (const [key, label, fmt] of COMPARE_METRICS) {
    appendRow(tbody, [label].concat(runs.map((r) => fmtMetric(metricOf(r, key), fmt))));
  }
  appendRow(tbody, ["参数"].concat(runs.map((r) => JSON.stringify(r.params || {}))));
  document.getElementById("compare-table-empty").hidden = true;
  table.hidden = false;
  document.getElementById("compare-curve-wrap").hidden = false;
  renderCompareLegend(runs, colors);
  drawMultiCurve(runs.map((r, i) => ({label: runLabel(r), color: colors[i], points: r.equity_curve || []})), "compare-canvas");
}

async function openCompare() {
  const hint = document.getElementById("compare-hint");
  const errorEl = document.getElementById("compare-error");
  if (compareSelection.length !== 2) {
    hint.textContent = "请选择恰好两条回测进行对比。";
    hint.hidden = false;
    return;
  }
  hint.hidden = true;
  try {
    const runs = await Promise.all(compareSelection.map((id) => fetchJSON("/v1/backtests/" + id)));
    clearError(errorEl);
    renderCompare(runs);
  } catch (err) {
    const compare = document.getElementById("compare");
    showError(errorEl, err);
    compare.hidden = false;
    compare.scrollIntoView();
  }
}

let curvePoints = [];
let curveIndex = 0;

function renderCurve(hoverIdx) {
  drawEquityCurve(curvePoints, hoverIdx);
  const readout = document.getElementById("curve-readout");
  if (hoverIdx === null || hoverIdx === undefined) {
    readout.hidden = true;
    return;
  }
  const p = curvePoints[hoverIdx];
  readout.textContent = String(p.ts).slice(0, 19) + " · " + fmtMoney(p.equity);
  readout.hidden = false;
}

function curveIndexAtX(canvas, clientX) {
  if (curvePoints.length < 2) return 0;
  const rect = canvas.getBoundingClientRect();
  const px = (clientX - rect.left) * (canvas.width / rect.width);
  const plotW = canvas.width - CURVE_PAD.left - CURVE_PAD.right;
  const frac = (px - CURVE_PAD.left) / plotW;
  return Math.max(0, Math.min(curvePoints.length - 1, Math.round(frac * (curvePoints.length - 1))));
}

function initResultsPage() {
  const listError = document.getElementById("results-error");
  if (!listError) return;
  setupBacktestRunForm();
  let resultsItems = [];
  const resultsSorter = makeTableSorter("results-table", RESULTS_SORT_KEYS);
  /* 过滤:输入防抖(250ms)后走服务端全库搜索(q 参数,ILIKE 包含匹配),
     清空恢复最近列表;applyFilter 仅做已加载数据的即时反馈,权威结果
     以服务端为准。排序保持跨页语义。 */
  const filterInput = document.getElementById("results-filter");
  const emptyEl = document.getElementById("results-empty");
  const EMPTY_DEFAULT = emptyEl.textContent;
  const applyFilter = () => {
    const q = filterInput.value.trim().toLowerCase();
    const list = q === "" ? resultsItems : resultsItems.filter((it) =>
      String(it.symbol).toLowerCase().includes(q) || String(it.strategy).toLowerCase().includes(q));
    emptyEl.textContent = q === "" ? EMPTY_DEFAULT : "无匹配「" + q + "」的回测结果。";
    renderResultsList(resultsSorter.sortItems(list), (item) => openDetail(item.id));
  };
  let filterTimer = null;
  filterInput.addEventListener("input", () => {
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      if (filterInput.value.trim() === "") {
        loadSorted();
      } else {
        applyFilter();
        loadSorted();
      }
    }, 250);
  });
  /* 跨页排序/搜索:表头点击或搜索词 → 服务端按全库数据重排
     (sort/order/q 参数),本地 sortItems 仅兜底。无排序参数时保持
     API 最新优先顺序。 */
  const PAGE_SIZE = 50;
  const moreWrap = document.getElementById("results-more-wrap");
  const moreBtn = document.getElementById("results-more");
  const updateMoreState = () => {
    /* 满页(50 的倍数)才可能有下一页 → 显示「加载更多」;搜索/尾页
       不满页或空列表时隐藏。 */
    moreWrap.hidden = resultsItems.length === 0 || resultsItems.length % PAGE_SIZE !== 0;
  };
  moreBtn.addEventListener("click", () => {
    const st = resultsSorter.state;
    const params = [];
    if (st.key) params.push("sort=" + st.key + "&order=" + (st.dir === 1 ? "asc" : "desc"));
    const q = filterInput.value.trim();
    if (q) params.push("q=" + encodeURIComponent(q));
    params.push("offset=" + resultsItems.length);
    moreBtn.disabled = true;
    moreBtn.textContent = "加载中…";
    fetchJSON("/v1/backtests?limit=" + PAGE_SIZE + "&" + params.join("&"))
      .then((items) => {
        resultsItems = resultsItems.concat(items);
        applyFilter();
        updateMoreState();
      })
      .catch((err) => showError(listError, err))
      .finally(() => {
        moreBtn.disabled = false;
        moreBtn.textContent = "加载更多";
      });
  });
  const loadSorted = () => {
    const st = resultsSorter.state;
    const params = [];
    if (st.key) params.push("sort=" + st.key + "&order=" + (st.dir === 1 ? "asc" : "desc"));
    const q = filterInput.value.trim();
    if (q) params.push("q=" + encodeURIComponent(q));
    loadJSON("/v1/backtests?limit=" + PAGE_SIZE + (params.length === 0 ? "" : "&" + params.join("&")), listError, (items) => {
      resultsItems = items;
      applyFilter();
      updateMoreState();
    });
  };
  resultsSorter.render = loadSorted;
  loadSorted();
  document.getElementById("detail-back").addEventListener("click", () => {
    document.getElementById("detail").hidden = true;
    document.getElementById("list").scrollIntoView();
  });
  document.getElementById("compare-btn").addEventListener("click", openCompare);
  document.getElementById("compare-back").addEventListener("click", () => {
    document.getElementById("compare").hidden = true;
    document.getElementById("list").scrollIntoView();
  });
  const canvas = document.getElementById("equity-canvas");
  canvas.addEventListener("mousemove", (event) => {
    if (curvePoints.length === 0) return;
    curveIndex = curveIndexAtX(canvas, event.clientX);
    renderCurve(curveIndex);
  });
  canvas.addEventListener("mouseleave", () => renderCurve(null));
  canvas.addEventListener("keydown", (event) => {
    if (curvePoints.length === 0) return;
    if (event.key === "ArrowRight") {
      curveIndex = Math.min(curvePoints.length - 1, curveIndex + 1);
      renderCurve(curveIndex);
    } else if (event.key === "ArrowLeft") {
      curveIndex = Math.max(0, curveIndex - 1);
      renderCurve(curveIndex);
    }
  });
  /* 一键回测跳入(watchlist 页 #bt-<id>):直接打开该回测详情。 */
  const bt = location.hash.match(/^#bt-(\d+)$/);
  if (bt) openDetail(Number(bt[1]));
}

/* 策略页策略说明卡(/v1/strategies schema):名称 + 描述 + 每参数
   「默认值 = 参数名 · 含义」,点击卡片联动下方编辑表单。 */

function renderStrategyCards(list) {
  const wrap = document.getElementById("strategy-cards");
  if (!wrap) return;
  wrap.replaceChildren();
  for (const s of list) {
    const card = document.createElement("article");
    card.className = "strategy-card";
    card.dataset.strategy = s.name;
    const h3 = document.createElement("h3");
    h3.textContent = s.name;
    card.appendChild(h3);
    const desc = document.createElement("p");
    desc.className = "strategy-desc";
    desc.textContent = s.description || "";
    card.appendChild(desc);
    const dl = document.createElement("dl");
    for (const p of s.params) {
      const dt = document.createElement("dt");
      dt.textContent = p.name + " = " + (p.default === undefined || p.default === null ? "—" : p.default);
      const dd = document.createElement("dd");
      dd.textContent = p.description || "";
      dl.appendChild(dt);
      dl.appendChild(dd);
    }
    card.appendChild(dl);
    wrap.appendChild(card);
  }
}

/* 导航高亮当前页:按 pathname 匹配 nav 链接加 active 类(/ui/ 直达
   与 index.html 同页)。 */
function setActiveNav() {
  let path = location.pathname.replace(/\/+$/, "");
  if (path === "/ui/index.html") path = "/ui"; /* /ui/ 与 /ui/index.html 同页 */
  for (const a of document.querySelectorAll("header nav a")) {
    let want = new URL(a.getAttribute("href"), location.origin).pathname.replace(/\/+$/, "");
    if (want === "/ui/index.html") want = "/ui";
    a.classList.toggle("active", want === path);
  }
}

initTheme();
initDashboardPage();
initAdminPage();
initWatchlistPage();
initDataPage();
initResultsPage();
setActiveNav();
