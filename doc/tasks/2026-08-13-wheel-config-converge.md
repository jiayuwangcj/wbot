# wheel 配置收敛:去除 lot_size,价格范围+最大持仓

**State**: DONE(2026-08-13,提交 5e3d26e + 文档 35e15b1)

## Goal

老板三条连续指令(2026-08-13):
1. 「去除 lot_size 这类可以直接拉到的」——合约乘数实时从行情拉取
2. 「设置价格范围和最大持仓,两个值共同作用,其他参数根据实时情况(期权价格等级、合约乘数等)都自动推断」
3. 「如何分配持仓价格,都交由策略自主决定」

## Constraints

- 存量 DB 配置(00883/09988)params 带 `lot_size:100` 键,buildParams unknown-param 校验不得拒绝 → ParseConfig `delete(params, "lot_size")` 静默兼容
- 前端旧多锚点曲线数据回填兼容(端点塌缩)
- 基准价语义:曲线第一点=最低价=满仓(max_inventory);最后点=最高价=清仓(0);区间内线性插值 = 策略自主分配

## Changes

后端:
- `wheel.Config` 删 LotSize;`DefaultLotSize=100` 导出常量(持仓推算兜底);`candidateLotSize` 取第一个有效候选 lot,行情为准
- `strategy.go` wheel 模板删 lot_size Param;ParseConfig delete 兼容存量
- `quote_collection.go` 用 `wheel.DefaultLotSize`
- 测试:wheel_test 「mismatched lot accepted」、strategy_test 遗留键忽略、httpapi 「bad param type」改用 max_inventory + 新增 TestBacktestExecuteLegacyLotSizeIgnored

前端:
- `WheelParams` 类型收敛:仅 curve + max_inventory 必填,其余可选,lot_size 移除
- `WheelForm` 三字段(价格下限/上限/最大库存)→ 两点曲线;旧曲线回填端点
- watchlist 徽标「wheel · 价格 100–120 · 最大库存 100」;trace 不再回填 lot_size/strategic_state

文档:API.md/WHEEL_STRATEGY.md/BACKTEST.md 参数表与示例去 lot_size,记录价格范围语义。

## Verify

- verify.sh 全绿(gofmt/vet/race/staticcheck/CLI smoke + 前端 build + vitest 76 用例)
- httpapi 新增遗留键用例通过

## Next

- 部署 serve 容器后验证 00883/09988 存量配置(含 lot_size 键)正常轮询、无 config error
