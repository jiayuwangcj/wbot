#!/usr/bin/env bash
# Acceptance: futu 数据面 HTTP 端点 (S-futu-data).
# Verifies the gateway-backed GET /v1/futu/quote|orders|account real contract
# (成功形状 + env/acc_id/pending 参数通道 + 错误契约), closing the #47
# 验收分层(子命令级 vs 数据面级)gap: futu 系端点此前只有 doc/API.md 契约
# 与单测,零 e2e 脚本(dev-up 刻意不含网关依赖,注释写明由 accept 覆盖)。
#
# Usage: scripts/accept-futu-data.sh [base-url]
# Requires: curl, node, a running `wbot serve` with the futu gateway reachable
#   (scripts/dev-up.sh 起的环境; 网关不可达时成功项会 503 而失败)。
set -uo pipefail
base="${1:-http://127.0.0.1:8080}"
pass=0; failed=0
check() { local d="$1" w="$2" g="$3"; if [[ "$g" == "$w" ]]; then pass=$((pass+1)); printf '  \033[32mPASS\033[0m %s\n' "$d"; else failed=$((failed+1)); printf '  \033[31mFAIL\033[0m %s (want %s, got %s)\n' "$d" "$w" "$g"; fi; }
strip_ansi() { sed -E 's/\x1b\[[0-9;]*m//g'; }

# 1. quote: 200 + 网关快照形状(basic_qot_list ≥1, cur_price 数值, name 字符串)。
body="$(curl -s -m 10 "$base/v1/futu/quote?symbol=HK.00700")"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/futu/quote?symbol=HK.00700")"
check "GET /v1/futu/quote?symbol=HK.00700 → 200 (got $code)" 200 "$code"
node -e "
const j = JSON.parse(process.argv[1]);
const q = (j.basic_qot_list || [])[0];
process.exit(q && typeof q.cur_price === 'number' && typeof q.name === 'string' ? 0 : 1);
" "$body"
check "quote 形状: basic_qot_list ≥1 + cur_price/name" 0 "$?"

# 2. quote 错误契约: 缺 symbol → 400;非法 symbol → 400。
code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$base/v1/futu/quote")"
check "quote 缺 symbol → 400 (got $code)" 400 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$base/v1/futu/quote?symbol=bogus")"
check "quote symbol=bogus → 400 (got $code)" 400 "$code"

# 3. orders: 200 + 快照形状(env/acc_id/orders 数组;非空时首行白名单键)。
body="$(curl -s -m 10 "$base/v1/futu/orders")"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/futu/orders")"
check "GET /v1/futu/orders → 200 (got $code)" 200 "$code"
node -e "
const j = JSON.parse(process.argv[1]);
if (j.env !== 'simulate' || typeof j.acc_id !== 'number' || !Array.isArray(j.orders)) process.exit(1);
if (j.orders.length && !j.orders.every(o => 'order_id' in o && 'symbol' in o && 'status' in o && 'side' in o)) process.exit(1);
process.exit(0);
" "$body"
check "orders 形状: env/acc_id/orders 数组 + 白名单键" 0 "$?"

# 4. orders 参数通道: env=real 生效;错误契约 env/acc_id/pending。
body="$(curl -s -m 10 "$base/v1/futu/orders?env=real")"
node -e "const j=JSON.parse(process.argv[1]); process.exit(j.env === 'real' ? 0 : 1);" "$body"
check "orders env=real 生效 (got $(echo "$body" | node -e "const j=JSON.parse(require('fs').readFileSync(0)); console.log(j.env)" | strip_ansi))" 0 "$?"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$base/v1/futu/orders?env=bogus")"
check "orders env=bogus → 400 (got $code)" 400 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$base/v1/futu/orders?acc_id=abc")"
check "orders acc_id=abc → 400 (got $code)" 400 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$base/v1/futu/orders?pending=2")"
check "orders pending=2 → 400 (got $code)" 400 "$code"

# 5. account: 200 + 快照形状(funds 白名单键 + total_assets>0 + positions 数组)。
body="$(curl -s -m 10 "$base/v1/futu/account")"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "$base/v1/futu/account")"
check "GET /v1/futu/account → 200 (got $code)" 200 "$code"
node -e "
const j = JSON.parse(process.argv[1]);
const f = j.funds || {};
const ok = j.env === 'simulate' && typeof j.acc_id === 'number'
  && ['total_assets','cash','market_val','available_cash','power'].every(k => typeof f[k] === 'number')
  && f.total_assets > 0 && Array.isArray(j.positions);
process.exit(ok ? 0 : 1);
" "$body"
check "account 形状: funds 白名单键 + total_assets>0 + positions 数组" 0 "$?"

# 6. account env=real 通道 + 错误契约。
body="$(curl -s -m 10 "$base/v1/futu/account?env=real")"
node -e "const j=JSON.parse(process.argv[1]); process.exit(j.env === 'real' && j.funds.total_assets > 0 ? 0 : 1);" "$body"
check "account env=real 生效 + total_assets>0 (got $(echo "$body" | node -e "const j=JSON.parse(require('fs').readFileSync(0)); console.log(j.funds.total_assets)" | strip_ansi))" 0 "$?"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$base/v1/futu/account?env=bogus")"
check "account env=bogus → 400 (got $code)" 400 "$code"
code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$base/v1/futu/account?acc_id=abc")"
check "account acc_id=abc → 400 (got $code)" 400 "$code"

echo
if [[ "$failed" == "0" ]]; then
  printf '  \033[32mALL %d CHECKS PASSED\033[0m\n' "$pass"
else
  printf '  \033[31m%d/%d CHECKS FAILED\033[0m\n' "$failed" "$((pass + failed))"
  exit 1
fi
