"use strict";
document.documentElement.classList.add("js");

/* Shared helpers; per-page init no-ops when its elements are absent. */

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
    const td = document.createElement("td");
    td.textContent = cell;
    tr.appendChild(td);
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

let dashEnv = "sim"; /* 当前明细视图(聚合卡/持仓/订单) */
const snapByEnv = {sim: null, real: null};
const errByEnv = {sim: null, real: null};

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
  renderTable("positions-table", snap.positions.map((p) => [p.symbol, p.qty, p.avg_cost, p.price, p.market_val, p.pl]));
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
  renderTable("orders-table", snap.orders.map((o) => {
    const sideTd = document.createElement("td");
    sideTd.textContent = o.side;
    sideTd.className = o.side.toLowerCase() === "buy" ? "side-buy" : "side-sell";
    return [o.create_time, o.symbol, sideTd, o.status, o.qty, o.price, o.fill_qty];
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
  await Promise.all([loadEnvSnap("sim"), loadEnvSnap("real")]);
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
}

function renderRuns(runs) {
  renderTable("runs-table", runs.map((r) => [r.id, r.source, r.status, r.started_at, r.finished_at === null ? "running" : r.finished_at]));
}

function initDashboardPage() {
  const paperBtn = document.getElementById("env-paper");
  if (!paperBtn) return;
  const refresh = document.getElementById("dash-refresh");
  const buttons = [paperBtn, document.getElementById("env-real")];
  for (const btn of buttons) {
    btn.addEventListener("click", () => {
      dashEnv = btn.id === "env-real" ? "real" : "sim";
      renderBadge();
      renderSummary();
      renderPositions();
      loadOrders();
    });
  }
  refresh.addEventListener("click", loadDashboard);
  loadDashboard();
  loadJSON("/v1/runs?limit=10", document.getElementById("runs-error"), renderRuns);
}

/* Admin page: /v1/admin/status, /v1/admin/cluster, /v1/admin/config (read-only). */

function dbState(db) {
  let state = db.ok ? "ok" : "down";
  if (typeof db.latency_ms === "number") {
    state += " (" + db.latency_ms + " ms)";
  }
  return state;
}

function renderStatus(s) {
  setText("status-version", s.version);
  setText("status-pid", s.pid);
  setText("status-started", s.started_at);
  setText("status-uptime", s.uptime_seconds + " s");
  setText("status-listen", s.listen_addr);
  setText("status-db", dbState(s.db));
  document.getElementById("status-list").hidden = false;
}

function renderCluster(c) {
  const comps = c.components;
  setText("cluster-process-version", comps.process.version);
  setText("cluster-process-pid", comps.process.pid);
  setText("cluster-process-started", comps.process.started_at);
  setText("cluster-process-uptime", comps.process.uptime_seconds + " s");
  setText("cluster-process-listen", comps.process.listen_addr);
  setText("cluster-db-state", dbState(comps.db));
  setText("cluster-db-latency", typeof comps.db.latency_ms === "number" ? comps.db.latency_ms + " ms" : "n/a");
  setText("cluster-pipeline-running", comps.pipeline.counts.running);
  setText("cluster-pipeline-succeeded", comps.pipeline.counts.succeeded);
  setText("cluster-pipeline-failed", comps.pipeline.counts.failed);
  renderTable("cluster-pipeline-runs-table", comps.pipeline.recent_runs.map((r) => [r.id, r.source, r.status, r.started_at, r.finished_at === null ? "running" : r.finished_at]));
  renderCoverageTable("cluster-coverage-table", comps.data_plane.bars_coverage);
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
    td.textContent = "fresh";
  }
  return td;
}

function renderCoverageTable(id, rows) {
  const table = document.getElementById(id);
  const empty = document.getElementById(id.replace("-table", "-empty"));
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
  if (rows.length === 0) {
    table.hidden = true;
    empty.hidden = false;
    return;
  }
  for (const b of rows) {
    const tr = document.createElement("tr");
    for (const cell of [b.symbol, b.timeframe, b.count, b.min_ts, b.max_ts]) {
      const td = document.createElement("td");
      td.textContent = cell;
      tr.appendChild(td);
    }
    tr.appendChild(freshnessCell(b));
    tbody.appendChild(tr);
  }
  empty.hidden = true;
  table.hidden = false;
}

function renderConfig(keys) {
  renderTable("config-table", keys.map((c) => [c.key, c.group, c.set ? "yes" : "no", c.updated_at === null ? "not set" : c.updated_at]));
}

function initAdminPage() {
  const statusError = document.getElementById("status-error");
  if (!statusError) return;
  loadJSON("/v1/admin/status", statusError, renderStatus);
  loadJSON("/v1/admin/cluster", document.getElementById("cluster-error"), renderCluster);
  loadJSON("/v1/admin/config", document.getElementById("config-error"), renderConfig);
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
  const table = document.getElementById("coverage-table");
  const empty = document.getElementById("coverage-empty");
  const tbody = table.tBodies[0];
  tbody.replaceChildren();
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
    const cells = [b.symbol, b.timeframe, b.adjust, b.count, b.min_ts.slice(0, 16), b.max_ts.slice(0, 16), fmtAge(b.max_ts_age_seconds)];
    for (const cell of cells) {
      const td = document.createElement("td");
      td.textContent = cell;
      tr.appendChild(td);
    }
    tr.appendChild(freshnessCell(b));
    tbody.appendChild(tr);
  }
  empty.hidden = true;
  table.hidden = false;
}

async function loadDataCoverage() {
  const data = await fetchJSON("/v1/admin/cluster");
  renderCoverageRows(data.components.data_plane.bars_coverage || []);
}

function fmtNum(v) {
  return Number(v).toLocaleString("en-US", { maximumFractionDigits: 2 });
}

function drawSparkline(canvas, closes) {
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
  ctx.strokeStyle = closes[closes.length - 1] >= closes[0] ? "#1a7f37" : "#cf222e";
  ctx.lineWidth = 2;
  ctx.beginPath();
  for (let i = 0; i < closes.length; i++) {
    const x = pad + (i / (closes.length - 1)) * (width - 2 * pad);
    const y = pad + (1 - (closes[i] - min) / span) * (height - 2 * pad);
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  }
  ctx.stroke();
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
  for (const b of bars) {
    appendRow(tbody, [b.ts.slice(0, 16).replace("T", " "), fmtNum(b.open), fmtNum(b.high), fmtNum(b.low), fmtNum(b.close), b.volume]);
  }
  empty.hidden = true;
  table.hidden = false;
}

async function loadBars(symbol, timeframe, adjust) {
  const errEl = document.getElementById("detail-error");
  clearError(errEl);
  const empty = document.getElementById("detail-empty");
  empty.hidden = true;
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
  document.getElementById("data-refresh").addEventListener("click", () => {
    const errEl = document.getElementById("data-error");
    clearError(errEl);
    loadDataCoverage().catch((err) => showError(errEl, err));
  });
  loadDataCoverage().catch((err) => showError(document.getElementById("coverage-error"), err));
}

/* Watchlist page: /v1/watchlist CRUD + /v1/strategies schema-driven param form. */

function renderParamFields(strategy, values) {
  const fields = document.getElementById("param-fields");
  fields.replaceChildren();
  const legend = document.createElement("legend");
  legend.textContent = "Parameters";
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

function renderWatchlist(items, onEdit, onDelete) {
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
    edit.textContent = "Edit";
    edit.addEventListener("click", () => onEdit(item));
    const del = document.createElement("button");
    del.type = "button";
    del.className = "link danger";
    del.textContent = "Delete";
    del.addEventListener("click", () => onDelete(item));
    actions.appendChild(edit);
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
  const listError = document.getElementById("watchlist-error");
  let strategies = [];
  let editingSymbol = null;

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
          document.getElementById("editor").scrollIntoView();
        });
      }
    });
  }

  function loadWatchlist() {
    loadJSON("/v1/watchlist", listError, (items) => {
      renderWatchlist(items, beginEdit, deleteItem);
    });
  }

  function beginEdit(item) {
    editingSymbol = item.symbol;
    form.symbol.value = item.symbol;
    strategySelect.value = item.strategy;
    renderParamFields(currentStrategy(), item.params);
    clearError(formError);
    document.getElementById("editor").scrollIntoView();
  }

  function resetForm() {
    editingSymbol = null;
    form.symbol.value = "";
    renderParamFields(currentStrategy());
    form.symbol.focus();
  }

  async function deleteItem(item) {
    if (!confirm("Remove " + item.symbol + " from the watchlist?")) return;
    try {
      await fetchJSON("/v1/watchlist/" + encodeURIComponent(item.symbol), {method: "DELETE"});
      loadWatchlist();
    } catch (err) {
      showError(listError, err);
    }
  }

  strategySelect.addEventListener("change", () => renderParamFields(currentStrategy()));
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const symbol = form.symbol.value.trim();
    if (!symbol) {
      showError(formError, new Error("symbol is required"));
      return;
    }
    const strategy = currentStrategy();
    if (!strategy) {
      showError(formError, new Error("select a strategy"));
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
      resetForm();
      loadWatchlist();
    } catch (err) {
      showError(formError, err);
    }
  });

  loadStrategies();
  loadWatchlist();
}

/* Results page: /v1/backtests list + detail with a hand-drawn equity curve. */

const CURVE_PAD = {top: 12, right: 64, bottom: 26, left: 48};

function cssVar(name) {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name);
  return raw ? raw.trim() : null;
}

/* palette from style.css vars, hex fallbacks */
const CURVE_LINE = cssVar("--accent") || "#0969da";
const CURVE_ALT = cssVar("--accent-2") || "#eb6834";
const CURVE_GRID = cssVar("--border") || "#d0d7de";
const CURVE_TEXT = cssVar("--muted") || "#656d76";

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
    hint.textContent = "Select exactly two runs to compare.";
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
    const cells = [
      item.id,
      item.strategy,
      item.symbol,
      fmtMetric(metricOf(item, "equity"), fmtMoney),
      fmtMetric(metricOf(item, "total_return"), fmtPct),
      fmtMetric(metricOf(item, "max_drawdown"), fmtPct),
      fmtMetric(metricOf(item, "bars"), String),
      item.created_at
    ];
    for (const cell of cells) {
      const td = document.createElement("td");
      td.textContent = cell;
      tr.appendChild(td);
    }
    const actions = document.createElement("td");
    const open = document.createElement("button");
    open.type = "button";
    open.className = "link";
    open.textContent = "Detail";
    open.addEventListener("click", () => onOpen(item));
    actions.appendChild(open);
    tr.appendChild(actions);
    tbody.appendChild(tr);
  }
  empty.hidden = true;
  table.hidden = false;
}

function showMetric(id, v, formatter) {
  setText(id, fmtMetric(v, formatter));
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
  renderTable("trades-table", (d.trades || []).map((t) => [t.ts, t.action, t.symbol, t.size, t.price, t.cash_after]));
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

function openDetail(id) {
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
  ctx.strokeStyle = CURVE_GRID;
  ctx.fillStyle = CURVE_TEXT;
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
    ctx.fillStyle = CURVE_TEXT;
    ctx.textAlign = "left";
    ctx.textBaseline = "middle";
    ctx.fillText(fmtMoney(pts[last].equity), x(Date.parse(pts[last].ts)) + 10, y(pts[last].equity));
  }
  if (hover === null || hover === undefined) return;
  const hp = series[0].points[hover];
  ctx.lineWidth = 1;
  ctx.strokeStyle = CURVE_GRID;
  ctx.beginPath();
  ctx.moveTo(x(Date.parse(hp.ts)), CURVE_PAD.top);
  ctx.lineTo(x(Date.parse(hp.ts)), CURVE_PAD.top + h);
  ctx.stroke();
  ctx.beginPath();
  ctx.arc(x(Date.parse(hp.ts)), y(hp.equity), 4, 0, 2 * Math.PI);
  ctx.fillStyle = CURVE_LINE;
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
  drawCurvePlot(canvas, [{points: points, color: CURVE_LINE}], hoverIdx);
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
  drawCurvePlot(canvas, series, null);
}

/* Compare view: side-by-side metrics + overlaid equity curves with legend. */

function runLabel(r) {
  return "#" + r.id + " " + r.strategy + " " + r.symbol;
}

const COMPARE_METRICS = [
  ["equity", "Equity", fmtMoney],
  ["total_return", "Total return", fmtPct],
  ["max_drawdown", "Max drawdown", fmtPct],
  ["bars", "Bars", String]
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
  const colors = [CURVE_LINE, CURVE_ALT];
  const table = document.getElementById("compare-table");
  const headRow = table.tHead.rows[0];
  headRow.replaceChildren();
  const metricHead = document.createElement("th");
  metricHead.textContent = "Metric";
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
  appendRow(tbody, ["Params"].concat(runs.map((r) => JSON.stringify(r.params || {}))));
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
    hint.textContent = "Select exactly two runs to compare.";
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
  loadJSON("/v1/backtests?limit=50", listError, (items) => {
    renderResultsList(items, (item) => openDetail(item.id));
  });
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

initDashboardPage();
initAdminPage();
initWatchlistPage();
initDataPage();
initResultsPage();
