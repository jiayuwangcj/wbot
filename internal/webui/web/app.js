"use strict";
document.documentElement.classList.add("js");

/* Shared helpers; per-page init no-ops when its elements are absent. */

async function fetchJSON(url) {
  let resp;
  try {
    resp = await fetch(url);
  } catch (err) {
    throw new Error("cannot reach the server");
  }
  if (!resp.ok) {
    let msg = "HTTP " + resp.status;
    try {
      const body = await resp.json();
      if (body && typeof body.error === "string") {
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

function initDataPage() {
  const barsForm = document.getElementById("bars-form");
  if (!barsForm) return;
  const barsError = document.getElementById("bars-error");
  barsForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const params = new URLSearchParams();
    params.set("symbol", barsForm.symbol.value.trim());
    params.set("timeframe", barsForm.timeframe.value.trim());
    const from = toRFC3339(barsForm.from.value);
    const to = toRFC3339(barsForm.to.value);
    if (from !== "") params.set("from", from);
    if (to !== "") params.set("to", to);
    try {
      const bars = await fetchJSON("/v1/bars?" + params.toString());
      clearError(barsError);
      renderBars(bars);
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

initDataPage();
initAdminPage();
