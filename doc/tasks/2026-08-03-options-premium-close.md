# 闭环: 期权链日 K 收盘权利金 (P3a)

- **日期**: 2026-08-03
- **PR**: #299(本体)+ 本文档(归档)
- **背景**: 候选池枯竭后,对账引擎系统化扫描全部文档排期标记(FUTU.md/API.md 的「P3 排期」),发现期权链权利金是文档明确排期项且可拆半:`option_quotes` 已存每合约日 K(ingest futu-option 落库),日 K 收盘价即权利金收盘价——P3a 零网关成本;实时 option-quote/IV 仍 P3。

## 改动

- `internal/ingest/futu_option.go`: `QueryLatestOptionQuote`(按合约取最近一行,无行 → nil)
- `internal/httpapi/httpapi.go`: `Store.LatestOptionQuote` + dbStore 委托
- `internal/httpapi/futu_options.go`: `FutuOptionPremier` 变参注入(可选;纯网关模式形状不变);contracts 加 `premium_close`/`premium_close_ts`(omitempty;查询失败只打日志不破坏链)
- 契约测试 4 用例:有值 / 无数据缺省 / 查询错误不 500 / nil premier
- 文档:API.md contracts 字段表 + 权利金说明段、FUTU.md §7 代理表 + §10 P3a 段

## 验证

- verify.sh 连跑两遍全绿;CI 5/5(含 db-integration 真 PG)
- **端到端验收**(本地 serve + 真网关 + 真 PG):HK.00700 9 到期 98 合约全带 premium_close(价内 C335000 call=140.46、P335000 put=0.13,形态正确);无数据到期 2026-08-28 126 合约字段缺省
- 运维注记:OrbStack 下网关/DB 走 bridge IP(192.168.215.x)可达,host 127.0.0.1 端口映射失效(已知环境特性)

## 备注

- **引擎经验(排期项扫描)**: 候选池枯竭时,`grep '排期\|P[0-9] 排期' doc/*.md` 系统化列出文档排期项——它们是「有意排期」不欠账,但可拆出**半程最小步**(P3a/P3b)兑现文档承诺;本次 P3a 全程零网关新增调用。
- **契约经验**: 装饰字段(premium)必须 `omitempty` + 查询失败不破坏主响应——契约测试锁定「无数据 → 字段缺省」与「错误 → 仍 200」两条路径。
- 剩余排期:实时逐合约 option-quote/IV 填充(implied_vol 列)、限频池跨进程聚合、多 symbol 时间对齐(待拍板)。
