# bars 补数据端到端验收脚本沉淀 (S-bars-refill) — 2026-08-03

状态: ✅ 已合并 (PR #175)

## 背景
AUTO_ADVANCE 根任务循环。memory 中「暂缓:Data 补数据 201 端到端
(需网关凭证)」——bars「补数据」按钮(POST /v1/ingest)上线以来从未
做真实端到端验收,当时网关不可用而暂缓;闭环 #25 已证明网关可用
(期权链真实拉取 289s 成功),本闭环解锁该暂缓项,补上 bars 侧端到端
验收沉淀,与 scripts/accept-options-ingest.sh(kind=option)配对。

## 改动
1. **scripts/accept-bars-refill.sh**(新增,运维沉淀):4 检查——
   空 symbol 400 契约 / 非法 symbol 503 契约 / HK.00700 1d fwd 真实
   拉取 201 / 数据落库(bars rows/max_ts 拉取前后非下降)。
   - 与期权脚本同模式:真实拉取 + 数据落库断言;不做 fresh 断言
     (周末 max_ts 停在最后交易日,见 #25 备注)。
   - 实测:HK.00700 1d fwd 拉取 6.7s(远快于期权链),幂等
     (5449 → 5449)。

## 验证
- 逐端点验收 4/4(scripts/accept-bars-refill.sh)
- 错误契约:空 symbol → 400 invalid_request;`NOPE` → 503
  ingest_failed(内部 `futu source: bad symbol` 透传,action 提示
  CLI 命令)

## 备注
- **暂缓项解锁**:memory「Data 补数据 201 端到端(需网关凭证)」→
  ✅ 完成(PR #175)。「补数据」按钮(2026-08-02 上线)与「拉取期权链」
  按钮(#25)至此均有端到端验收脚本。
- 与 bars 相关待老板项不变:多 symbol 时间对齐(blocked 待拍板)。
