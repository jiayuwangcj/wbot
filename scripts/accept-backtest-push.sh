#!/usr/bin/env bash
# Acceptance: Discord report embed, retry, enforced nonce and durable report-ID idempotency.
# Uses a local fake Discord Bot API; no external credentials or network are required.
# Usage: scripts/accept-backtest-push.sh [wbot-bin]
set -uo pipefail

pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }
has() { local hay="$1" pat="$2"; if grep -qE "$pat" <<<"$hay"; then echo 1; else echo 0; fi; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp -d)"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT
if [[ -n "${1:-}" ]]; then WB="$1"; else go build -o "$tmp/wbot" ./cmd/wbot && WB="$tmp/wbot"; fi

mkdir -p "$tmp/config" "$tmp/reports"
printf '%s\n' '[{"ts":"2026-08-12T00:00:00Z","open":100,"high":101,"low":99,"close":100,"volume":1000},{"ts":"2026-08-13T00:00:00Z","open":100,"high":102,"low":99,"close":101,"volume":1100}]' >"$tmp/bars.json"
printf '%s\n' '{"credentials.discord.bot_token":{"value":"fake-token","updated_at":"2026-08-14T00:00:00Z"},"credentials.discord.channel_id":{"value":"channel-7","updated_at":"2026-08-14T00:00:00Z"}}' >"$tmp/config/wbot.conf"
chmod 600 "$tmp/config/wbot.conf"

node -e '
const http=require("http"),fs=require("fs");
const log=process.argv[1],portFile=process.argv[2]; let attempts=0;
const server=http.createServer((req,res)=>{let body="";req.on("data",c=>body+=c);req.on("end",()=>{
  attempts++; let payload=null; try{payload=JSON.parse(body)}catch(e){}
  fs.appendFileSync(log,JSON.stringify({method:req.method,path:req.url,authorization:req.headers.authorization,payload})+"\n");
  if(attempts===1){res.statusCode=503;res.end("retry");return}
  res.setHeader("content-type","application/json");res.end("{\"id\":\"message-1\"}");
})});
server.listen(0,"127.0.0.1",()=>fs.writeFileSync(portFile,String(server.address().port)));
process.on("SIGTERM",()=>server.close(()=>process.exit(0)));
' "$tmp/requests.ndjson" "$tmp/port" </dev/null &
server_pid=$!
for _ in $(seq 1 100); do [[ -s "$tmp/port" ]] && break; sleep 0.05; done
if [[ ! -s "$tmp/port" ]]; then echo "fake Discord server failed to start" >&2; exit 1; fi
discord_base="http://127.0.0.1:$(<"$tmp/port")"
args=(backtest -file "$tmp/bars.json" -symbol HK.00700 -strategy hold -report -push -report-dir "$tmp/reports")

out1="$(WBOT_CONFIG_DIR="$tmp/config" DISCORD_API_BASE_URL="$discord_base" "$WB" "${args[@]}" 2>&1)"; code1=$?
json_path="$(find "$tmp/reports" -maxdepth 1 -name '*.json' -print -quit)"
html_path="${json_path%.json}.html"
failed_marker_count="$(find "$tmp/config" -type f -name '*.sent' | wc -l | tr -d ' ')"
check "首次 Discord 故障 → exit 1" 1 "$code1"
check "推送失败仍生成 JSON/HTML" 1 "$([[ -f "$json_path" && -f "$html_path" ]] && echo 1 || echo 0)"
check "失败原因可观测" 1 "$(has "$out1" 'push:.*HTTP status 503')"
check "失败不写 sent 标记" 0 "$failed_marker_count"

out2="$(WBOT_CONFIG_DIR="$tmp/config" DISCORD_API_BASE_URL="$discord_base" "$WB" "${args[@]}" 2>&1)"; code2=$?
sent_marker_count="$(find "$tmp/config" -type f -name '*.sent' | wc -l | tr -d ' ')"
check "同 report ID 重试成功" 1 "$([[ "$code2" == 0 && "$out2" == *'push_status=sent'* ]] && echo 1 || echo 0)"
check "成功后落一个 sent 标记" 1 "$sent_marker_count"

out3="$(WBOT_CONFIG_DIR="$tmp/config" DISCORD_API_BASE_URL="$discord_base" "$WB" "${args[@]}" 2>&1)"; code3=$?
check "第三次同 ID 幂等跳过" 1 "$([[ "$code3" == 0 && "$out3" == *'push_status=already_sent'* ]] && echo 1 || echo 0)"
id1="$(sed -n 's/.*report_id=\([^ ]*\).*/\1/p' <<<"$out1" | head -1)"
id2="$(sed -n 's/.*report_id=\([^ ]*\).*/\1/p' <<<"$out2" | head -1)"
id3="$(sed -n 's/.*report_id=\([^ ]*\).*/\1/p' <<<"$out3" | head -1)"
check "三次运行 report ID 一致" 1 "$([[ -n "$id1" && "$id1" == "$id2" && "$id2" == "$id3" ]] && echo 1 || echo 0)"

request_count="$(wc -l <"$tmp/requests.ndjson" | tr -d ' ')"
check "Discord 仅收到故障尝试 + 成功重试" 2 "$request_count"
nonce_ok="$(node -e 'const q=require("fs").readFileSync(process.argv[1],"utf8").trim().split("\n").map(JSON.parse);const a=q[0].payload,b=q[1].payload;process.stdout.write(String(a.enforce_nonce===true&&b.enforce_nonce===true&&typeof a.nonce==="string"&&a.nonce.length===25&&a.nonce===b.nonce));' "$tmp/requests.ndjson")"
check "重试使用同一 25 字符 enforced nonce" true "$nonce_ok"
embed_ok="$(node -e 'const q=require("fs").readFileSync(process.argv[1],"utf8").trim().split("\n").map(JSON.parse)[1].payload;const f=q.embeds[0].fields.map(x=>x.name);const want=["标的","数据窗口","权利金净额口径","已实现口径","有效覆盖率","费用","最大回撤","停止原因"];process.stdout.write(String(f.length===8&&want.every(x=>f.includes(x))));' "$tmp/requests.ndjson")"
check "Discord embed 含双口径等 8 个核心字段" true "$embed_ok"
risk_ok="$(node -e 'const fs=require("fs"),r=JSON.parse(fs.readFileSync(process.argv[1])),q=fs.readFileSync(process.argv[2],"utf8").trim().split("\n").map(JSON.parse)[1].payload;const d=q.embeds[0].description;process.stdout.write(String(r.risk.every(x=>d.includes(x))));' "$json_path" "$tmp/requests.ndjson")"
check "风险提示从 JSON 全量投影未截断" true "$risk_ok"
route_ok="$(node -e 'const q=require("fs").readFileSync(process.argv[1],"utf8").trim().split("\n").map(JSON.parse);process.stdout.write(String(q.every(x=>x.method==="POST"&&x.path==="/channels/channel-7/messages"&&x.authorization==="Bot fake-token")));' "$tmp/requests.ndjson")"
check "Bot token 与 channel 路由来自 wbot.conf" true "$route_ok"

echo
if [[ "$failed" == 0 ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass+failed))"
  exit 1
fi
