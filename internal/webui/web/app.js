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

/* Data page: /v1/bars on form submit, /v1/runs on load. */

function toRFC3339(dateValue) {
  return dateValue === "" ? "" : dateValue + "T00:00:00Z";
}

function renderBars(bars) {
  renderTable("bars-table", bars.map((b) => [b.ts, b.open, b.high, b.low, b.close, b.volume]));
}

function renderRuns(runs) {
  renderTable("runs-table", runs.map((r) => [r.id, r.source, r.status, r.started_at, r.finished_at === null ? "running" : r.finished_at]));
}

/* Bars coverage hint: /v1/admin/cluster bars_coverage (all adjusts), queried once on load. */
let barsCoverage = new Map();

function loadBarsCoverage() {
  fetchJSON("/v1/admin/cluster").then((c) => {
    for (const cov of (c.components.data_plane.bars_coverage || [])) {
      barsCoverage.set(cov.symbol + "|" + cov.timeframe, cov);
    }
  }).catch(() => {});
}

function showBarsCoverage(bars, symbol, timeframe) {
  const el = document.getElementById("bars-coverage");
  if (!el) return;
  const cov = barsCoverage.get(symbol + "|" + timeframe);
  if (cov) {
    el.textContent = "Data coverage: " + cov.count + " bars · " + cov.min_ts.slice(0, 10) + " → " + cov.max_ts.slice(0, 10) + " (ingested, all adjusts)";
  } else if (bars.length > 0) {
    el.textContent = "Query results: " + bars.length + " bars · " + bars[0].ts.slice(0, 10) + " → " + bars[bars.length - 1].ts.slice(0, 10);
  } else {
    el.textContent = "";
    el.hidden = true;
    return;
  }
  el.hidden = false;
}

function initDataPage() {
  const barsForm = document.getElementById("bars-form");
  if (!barsForm) return;
  const barsError = document.getElementById("bars-error");
  loadBarsCoverage();
  barsForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const symbol = barsForm.symbol.value.trim();
    const timeframe = barsForm.timeframe.value.trim();
    const params = new URLSearchParams();
    params.set("symbol", symbol);
    params.set("timeframe", timeframe);
    params.set("adjust", "fwd"); /* 数据标准默认前复权 (doc/DATA_STANDARD.md) */
    const from = toRFC3339(barsForm.from.value);
    const to = toRFC3339(barsForm.to.value);
    if (from !== "") params.set("from", from);
    if (to !== "") params.set("to", to);
    try {
      const bars = await fetchJSON("/v1/bars?" + params.toString());
      clearError(barsError);
      renderBars(bars);
      showBarsCoverage(bars, symbol, timeframe);
    } catch (err) {
      showError(barsError, err);
    }
  });
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
  renderTable("cluster-coverage-table", comps.data_plane.bars_coverage.map((b) => [b.symbol, b.timeframe, b.count, b.min_ts, b.max_ts]));
  document.getElementById("cluster-cards").hidden = false;
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
  ctx.textAlign = "center";
  ctx.textBaseline = "alphabetic";
  const first = series[0].points;
  for (const i of [0, Math.floor((first.length - 1) / 2), first.length - 1]) {
    ctx.fillText(String(first[i].ts).slice(0, 10), x(Date.parse(first[i].ts)), canvas.height - 8);
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
  const compare = document.getElementById("compare");
  compare.hidden = false;
  compare.scrollIntoView();
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
    showError(errorEl, err);
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

initDataPage();
initAdminPage();
initWatchlistPage();
initResultsPage();
