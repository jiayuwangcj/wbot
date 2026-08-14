# 任务:美股期权下单账户路由修复(TrdSecMarket/TrdMarket 枚举混用)

## Goal

用户点击 ✅ 对 `US.JD260821P29500` 下单失败:`place order failed: no simulate account for US.JD260821P29500 (option=true, market=11)`。修复账户自动路由,使美股期权(及美股正股)能落到正确的模拟账户。

## Constraints

- 模拟盘 fail-closed 语义不变:无匹配账户必须报错并列出可用账户,不冒名交易到错误账户。
- 港股/沪深既有路由(HK.00700→1907141、HK 期权→13477968、SH/SZ→1907143)不得回归。
- 验证必须先于提交:单元测试 + 真实网关只读验证,不下模拟单。

## Root Cause(两个叠加)

1. **枚举混用**:`AccountForSymbol` 用 ParseSymbol 的 **TrdSecMarket** 值(marketCode:HK=1 US=11 SH=21 SZ=22)直接匹配 `TrdMarketAuthList` 的 **TrdMarket** 枚举(HK=1 US=2 CN=3)。HK=1==1 巧合匹配(所以港股正常),US=11≠2 永不匹配(所以美股必失败)。测试 fixture 也按旧枚举写 11/21/22,与真实网关(markets=[2])不符,恰好掩盖了 bug。
2. **SimAccType=4 未文档化**:美股模拟账户 13477966 的 SimAccType=**4**,富途官方枚举仅 0-3(Unknown/Stock/Option/Futures)。原实现只放行 SimAccType=Option 账户做期权 → 4 被拒。

## Fix(commit dc38d1b,feat/llm-signal-endpoint)

- 新增 `secMarketToTrdMarket` 映射(1→1, 11→2, 21/22→3)用于 `TrdMarketAuthList` 匹配,与 `PlaceOrder` 内 `trdMarket` 同一映射。
- 证券类型过滤改为**排除式**:期权避开 SimAccType=Stock 专用股票账户,正股避开 SimAccType=Option 专用期权账户;其余类型(含 0/4 等未文档化值)按全能账户尝试,由市场授权过滤 + 网关侧拒绝兜底。白名单式(只放行 0/2)会在枚举再次扩展时静默断路由。
- `SimAccTypeName` 对 4 展示为 `all-in-one (SimAccType=4)`,避免再被显示成 unknown 误导排查。
- 测试 fixture 修正为真实网关形态(TrdMarket 枚举 + SimAccType=4),新增 US 期权路由正例 + 仅 HK 账户的 fail-closed 场景。

## Verify

- 单元测试:TestAccountForSymbol / TestAccountForSymbolFailClosed PASS。
- 真实网关只读验证(临时工具连 192.168.217.2:11111):
  - `US.JD260821P29500` / `US.AAPL` → acc=13477966(type=all-in-one, markets=[2])✓(修复前必失败)
  - `HK.00700` → 1907141、`HK.TCH260821P460000` → 13477968、`SH.600000` → 1907143 ✓ 无回归
- scripts/verify.sh 全绿(exit 0)。
- 部署:`docker compose --env-file ~/.wbot/serve.env -f configs/docker-compose.serve.yml up -d --build`,容器 healthy。

## State

**DONE**(2026-08-13,主会话响应 signal 739 ✅ 点击失败)。待观察:用户下次点击 ✅ 下单成功(真实落单验证)。

## Next

- 推送器 739 卡片实际丢失(旧容器「not yet recorded」卡死后游标已越过 739,新容器从 MaxSignalID 起不再补推)——若用户反馈未收到卡片,可考虑补推机制(需老板确认是否值得)。
- #56 LLM 审核请求瞬态失败(741 DNS 超时)被硬记 REJECTED:审核失败应重试而非直接落 REJECTED(排队中)。
