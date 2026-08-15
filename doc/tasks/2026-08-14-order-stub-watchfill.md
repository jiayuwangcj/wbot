# 下单留痕 + stub 订单号校验 + watchFill 未确认警示

**State**: 修复已提交 5c58192(老板 2026-08-14 00:35,另一会话),**待部署**——serve 容器 00:12:31 启动(650e189),未含新修复

## Goal

老板 2026-08-14 指令(源自 761 实测发现):模拟盘 stub 订单机制导致——① 多次 CONFIRM 返回同一 order_id(206158430256)与 `order_id_ex="0"`,DB 无法区分真实挂单;② watchFill 对无法确认的订单静默轮询,老板看不到警示。要求:下单留痕、stub 订单号校验、watchFill 未确认警示。

## 实测事实(2026-08-14 凌晨,serve 日志 + futu-opend-rs 网关日志)

- **futu-opend-rs 模拟盘 stub 机制**:PlaceOrder 返回 stub 占位(同 ID 复用:744/757/761 三次均 206158430256),`order_id_ex="0"`,`need_op_confirm=true`,`is_pending_broker_confirm=true`;**stub TTL 30s 后 purge**(网关日志 14:50:48/15:51:21/16:13:36 三次「pending stub TTL reached...count=0」),futu 侧实际无挂单、永不成交
- **DB pending 累积**:744/757/761 三条 CONFIRM 记录共存(ListPendingOrders 返回 3 条同 ID),LLM 审核输入随 pending 增长(761 审核输入 34KB)→ **762/763 审核连续超时(180s,deepseek-v4-pro 对大 payload 响应超时)**→ LLM_REVIEW_FAILED 语义为「跳过推送并推进游标」(discord_scheduler.go:252)→ **信号静默丢失,老板收不到卡片**
- **实测丢失链(2026-08-14)**:762 两次失败 16:15:54/16:18:57,763 失败 16:27:19(首次 16:27:19 + retrying once),764 失败 16:31:30——762/763 为 `context deadline exceeded`(Client.Timeout 180s),**764 为 `parse verdict JSON: unexpected end of JSON input`**(deepseek 对超长输入返回空/截断响应,HTTP 成功但 body 空);审核输入含 3 条 pending(744/757/761);deepseek API 最小请求 0.7s/200(API 健康,大 payload 行为不稳定:超时/空响应两模式)。**FAILED 信号下一轮评估创建新信号**(762→763→764 链),不干预每 ~5 分钟丢一个
- **撤单路由错位**:Discord ❌ 按钮用 `sig.Symbol`(US.JD 正股)调 CancelOrder → 路由**正股账户 1907141**;期权订单实际在**期权账户 13477966**(AccountForSymbol 按证券类型分流)→ 16:13:45 实测撤单失败「订单不在本地缓存」
- **store.go:1018 ListPendingOrders 错标**:合约/方向/数量读 `sig.Candidates[0]`(761 首位是被排除的 P29000),与实际下单合约(P28500,在 CONFIRM details.symbol)不符——763+ 审核会把 761 的单错标为 P29000

## Constraints

- 资金安全铁律:不引入自动下单;所有写操作仍经 LLM 审核 + 人工决定
- 模拟盘 stub 是 futu-opend-rs 既有机制,不改网关;在 wbot 侧识别/标注
- 不碰 wheel.go 评估逻辑(8d9180a/650e189 刚稳定);文件重叠先 grep
- 任务记录同步:本文 + #60

## 改动面(先 grep 确认不重叠)

- **internal/futu/trade.go**:PlaceOrder 返回时区分 stub(order_id_ex="0"/stub 特征)与真实订单;OrderRequest/返回结构补订单类型
- **cmd/wbot/telegram_scheduler.go + discord_scheduler.go**:CONFIRM details 留痕补全(账户 ID/订单类型/stub 标记);watchFill 对 `found=false`(订单查不到)显式推送警示(挂单未确认),不静默轮询;❌ 撤单按钮改传 CONFIRM details 里的实际合约(symbol)
- **internal/wheelstore/store.go**:ListPendingOrders 合约改读 CONFIRM details.symbol(或 accepted 候选),同 order_id 去重(只保留最近一条)
- **验收脚本**:scripts/accept-order-stub-watchfill.sh(单测断言 stub 识别/未确认警示/pending 去重/撤单路由合约正确)

## Verify(验收)

- gofmt/vet/test/race/staticcheck + scripts/verify.sh 全绿
- 单测:PlaceOrder stub 标记;watchFill found=false 推送警示;ListPendingOrders 同 ID 去重 + 合约取 details.symbol;❌ 撤单用合约路由
- 端到端(合入后):761 的 pending 在 ListPendingOrders 显示 P28500 且仅一条;762/763 审核输入不再含重复 pending

## Links

- 任务: #60(老板指令 2026-08-14)
- 相关: doc/tasks/2026-08-13-*.md(#57/#58/#59 资金安全铁律 650e189)
- 根因分析: 761 链路(LLM APPROVE 16:12:16 → CONFIRM 16:13:06 → stub 206158430256;762 审核超时 16:15:54/16:18:57)

## 实测状态更新(2026-08-14 00:45 北京)

- **审核恢复(已定格)**:764 第二次审核成功(16:33:57),765 审核成功(16:35:32)+ 老板 Discord 确认 CONFIRM(16:35:46,actor discord:1486343344065089648)→ stub 单 206158430256;766 审核成功 REJECTED(~16:51,4 条 pending 输入正常处理,模型正确识别重复敞口拒绝——业务拒绝非系统故障)。**审核链路自恢复完成,丢失定格为 762/763/764 三条**
- **5c58192 已部署(2026-08-14 00:44 北京,serve 重启 16:44:27Z)**:新镜像 7719de50;768(16:44:48 创建,重启后第一轮评估)审核成功 REJECTED(16:47:14,llm:deepseek-v4-pro「数据完整性与持仓方向存在冲突」);767(16:44:13,重启前创建)被中断成孤儿信号,推送游标跳过
- **假警报澄清**:重启后 httpapi 报「health ping failed: context canceled」系 nc 管道 FIN 半关闭触发请求 context 取消的探测假象;宿主 curl 完整连接 health=ok,DB/httpapi/wheelrun 全正常
- **存量 pending**:744/757/761/765 四条同 ID stub CONFIRM 记录并存(审核已恢复,清理紧迫性下降,但 ListPendingOrders 仍会返回 4 条)

## 新发现(2026-08-14 17:00 北京):769 推送失败永久丢卡 → 新任务 #62

- **769 全链**:创建 → 审核前双通道 6 次「LLM review not yet recorded, will retry」(游标保持)→ 16:55:28 APPROVE 记录(llm:deepseek-v4-pro)→ 16:56:11 `discord: create message: request failed` → **无重试,游标推进,769 卡片永久丢失**
- **根因**:discord_scheduler.go:284-286 pushEmbedDiscord 失败只 logf 后 return false(fail-fast),与「retryable prefix」注释语义不一致;telegram_scheduler.go:407 SendMessage 失败同病(769 telegram 恰成功)
- **修复**:#62(doc/tasks/2026-08-14-push-retry-on-failure.md)推送失败 → retry=true 保持游标 30s 重推;dismissed/REJECTED/过期仍永久跳过。已派 Claude coder(worktree push-retry)
- **770**:17:00 前后创建,审核中——若 Discord API 持续抖动,770+ 验证修复前可能继续丢

## Next

- **#62 修复合入部署**(推送失败重试;观察 770+ 推送)
- 存量 pending(744/757/761/765)清理:审核已恢复,等老板拍板(补 NO/REJECTED 标记或保留)
- 769 的 CONFIRM 观察:769 卡片未达老板,若老板从 telegram 确认,将是 5c58192 stub 检测实测
