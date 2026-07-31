"use strict";
document.documentElement.classList.add("js");

/* Data page: loads /v1/runs on start, queries /v1/bars on form submit. */

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

function renderBars(bars) {
  renderTable("bars-table", bars.map((b) => [b.ts, b.open, b.high, b.low, b.close, b.volume]));
}

function renderRuns(runs) {
  renderTable("runs-table", runs.map((r) => [r.id, r.source, r.status, r.started_at, r.finished_at === null ? "running" : r.finished_at]));
}

function toRFC3339(dateValue) {
  return dateValue === "" ? "" : dateValue + "T00:00:00Z";
}

const barsForm = document.getElementById("bars-form");
const barsError = document.getElementById("bars-error");
const runsError = document.getElementById("runs-error");

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

async function loadRuns() {
  try {
    const runs = await fetchJSON("/v1/runs?limit=10");
    clearError(runsError);
    renderRuns(runs);
  } catch (err) {
    showError(runsError, err);
  }
}

loadRuns();
