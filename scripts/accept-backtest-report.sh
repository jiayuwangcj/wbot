#!/usr/bin/env bash
# Acceptance: deterministic schema 1.3 single-run JSON + HTML report.
# Usage: scripts/accept-backtest-report.sh [wbot-bin]
set -uo pipefail

pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }
has() { local hay="$1" pat="$2"; if grep -qE "$pat" <<<"$hay"; then echo 1; else echo 0; fi; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if [[ -n "${1:-}" ]]; then WB="$1"; else go build -o "$tmp/wbot" ./cmd/wbot && WB="$tmp/wbot"; fi

printf '%s\n' '[{"ts":"2024-01-01T00:00:00Z","open":100,"high":101,"low":99,"close":100,"volume":1000},{"ts":"2024-01-02T00:00:00Z","open":100,"high":103,"low":100,"close":102,"volume":1200}]' >"$tmp/bars.json"

out1="$($WB backtest -file "$tmp/bars.json" -symbol HK.00883 -strategy hold -seed 42 -report -report-dir "$tmp/reports" 2>&1)"; code1=$?
check "报告 CLI → exit 0" 0 "$code1"
check "零尝试汇总为 N/A" 1 "$(has "$out1" '未成交 N/A\(无成交尝试\)')"
json_path="$(find "$tmp/reports" -maxdepth 1 -name '*.json' -print -quit)"
html_path="${json_path%.json}.html"
check "JSON 与 HTML 文件存在" 1 "$([[ -f "$json_path" && -f "$html_path" ]] && echo 1 || echo 0)"

json_ok="$(node -e 'const fs=require("fs"),r=JSON.parse(fs.readFileSync(process.argv[1])); const top=["schema_version","report_id","report_kind","initial_cash","identity","result","terminal","data_quality","audit","risk"]; process.stdout.write(String(top.every(k=>Object.hasOwn(r,k)) && r.schema_version==="1.3" && r.report_kind==="single_run"));' "$json_path")"
check "JSON schema 顶层键齐全" true "$json_ok"
field_ok="$(node -e 'const r=require(process.argv[1]),x=r.result;const ok=r.initial_cash===10000&&x.final_equity_amount===10000&&x.annualized_return_pct===0&&x.cost_drag.total_fees_amount===0&&x.cost_drag.cost_drag_pct===0&&x.cost_drag_return_pct===0;process.stdout.write(String(ok));' "$json_path")"
check "本金/期末权益/年化/损耗字段可复算" true "$field_ok"
typed_out="$($WB backtest -file "$tmp/bars.json" -cash 10000 -strategy buy-hold -fee 3 -fee-option-per-contract 21 -fee-stock-per-lot 70 -lot-size 100 2>&1)"; typed_code=$?
check "分类型费率 flag 优先且按手计费" 1 "$( [[ "$typed_code" == 0 && "$typed_out" == *'final_equity=10130'* && "$typed_out" == *'fees=70'* ]] && echo 1 || echo 0 )"
legacy_out="$($WB backtest -file "$tmp/bars.json" -cash 10000 -strategy buy-hold -fee 3 2>&1)"; legacy_code=$?
check "旧 -fee 固定单值仍兼容" 1 "$( [[ "$legacy_code" == 0 && "$legacy_out" == *'final_equity=10197'* && "$legacy_out" == *'fees=3'* ]] && echo 1 || echo 0 )"
model_ok="$(node -e 'const r=require(process.argv[1]);const m=r.result.unfilled_model,c=m.components;process.stdout.write(String(m.model_kind==="heuristic"&&m.model_version==="heuristic-1.0"&&typeof m.order_assumption==="string"&&c.spread_weight===.55&&c.volume_weight===.3&&c.oi_weight===.15));' "$json_path")"
check "unfilled_model 对象形状" true "$model_ok"
recompute_ok="$(node -e 'const r=require(process.argv[1]).result;const ok=r.attempt_count===r.fill_count+r.unfilled_count&&(r.attempt_count===0?r.unfilled_ratio===null:r.unfilled_ratio===r.unfilled_count/r.attempt_count);process.stdout.write(String(ok));' "$json_path")"
check "未成交口径可复算且零分母为 null" true "$recompute_ok"
summary_ok="$(node -e 'const r=require(process.argv[1]).result,o=process.argv[2];const amount=Math.abs(r.net_return_amount-r.net_return_pct*10000)<1e-9;const line=o.includes(`realized_return=${r.net_return_pct}`)&&o.includes(`mark_return=${r.window_mark_to_market_return_pct}`)&&o.includes(`max_drawdown=${r.max_drawdown_pct}`);process.stdout.write(String(amount&&line));' "$json_path" "$out1")"
check "JSON 收益金额与 CLI 汇总可复算" true "$summary_ok"
html="$(<"$html_path")"
html_one_line="$(tr '\n' ' ' <<<"$html")"
check "HTML 含首屏关键字段" 1 "$(has "$html_one_line" '窗口末估值变动.*最大回撤.*未成交率.*停止原因.*数据有效覆盖率.*窗口末未平仓腿')"
check "HTML 含 Discord 元数据" 1 "$(has "$html_one_line" 'theme-color.*og:title.*og:description')"

cp "$json_path" "$tmp/first.json"; cp "$html_path" "$tmp/first.html"
out2="$($WB backtest -file "$tmp/bars.json" -symbol HK.00883 -strategy hold -seed 42 -report -report-dir "$tmp/reports" 2>&1)"; code2=$?
check "同输入第二次运行 → exit 0" 0 "$code2"
check "同 ID 覆盖且 JSON/HTML 字节一致" 1 "$([[ "$out1" == "$out2" ]] && cmp -s "$tmp/first.json" "$json_path" && cmp -s "$tmp/first.html" "$html_path" && echo 1 || echo 0)"

echo
if [[ "$failed" == 0 ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi
