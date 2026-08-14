# 任务:LLM 审核失败处理(重试 + 不再冒充 REJECTED + 推送器识别)

## Goal

2026-08-13 事故链:signal 741 一次 DNS 超时(dial tcp api.deepseek.com)被 RecordLLMGate 硬记 REJECTED —— 用户看到「模型拒绝」卡片,实际是基础设施故障;742 的审核请求在容器重建时被杀(在途,无动作落库,信号丢失)。

修复:①审核请求失败不得冒充模型裁决 ②瞬态失败自动重试 ③推送器识别失败态,不推误导卡片、不卡游标。

## Root Cause

1. `RecordLLMGate`(internal/llmreview/gate.go):Review 返回 error 时 verdict 保持 REJECT、disposition=REJECTED 直接落库 —— 网络错误被当成模型拒绝,且落库后返回 nil err(调用方无从区分)。
2. 推送器(Discord/Telegram)`LatestLLMReview` ErrNotFound 分支:只认 REJECTED,失败态会落入「freshness 窗口内无限 retry」→ 卡死整条推送链。
3. 容器重建(部署)会杀掉在途审核请求,无动作落库 → 信号丢失(742),推送器游标从 MaxSignalID 重启后不会补推。

## Fix(commit f78e88b,feat/llm-signal-endpoint)

- **gate.go**:Review 请求失败(网络/DNS/超时/非法响应)→ disposition=`LLM_REVIEW_FAILED`(verdict 仍 REJECT,fail-closed 不 APPROVE;失败原因进 details.error)。
- **wheelrun/runner.go reviewAlert**:disposition=LLM_REVIEW_FAILED 时同步重试一次(3s 退避);重试仍失败保留 failed(审计保留两次错误)。RecordLLMGate 落库后返回 nil err,重试条件用 action 判断。
- **discord_scheduler.go / telegram_scheduler.go**:ErrNotFound 分支识别 LLM_REVIEW_FAILED → 跳过推送(不推「模型拒绝」卡片)、推进游标(不再无限 retry 卡链)。
- llmsignal 链共用 RecordLLMGate,自动获得 failed 语义(无独立改动)。

## Verify

- gate 测试:错误分支断言 LLM_REVIEW_FAILED + verdict REJECT + error 入 details。
- TestRunOnceLLMGateStates 适配重试语义(failed 场景请求数 +1、动作 +1 两条 failed)。
- scripts/verify.sh 全绿(exit 0);部署后容器 healthy,signal 743/744/745 正常评估。

## State

**DONE**(2026-08-13)。存量 741 的 REJECTED 记录不动 DB(已过推送游标,审计可查;改 DB 审计数据风险大于收益)。

## 补充修复(2026-08-13 当晚,commits 2fee07c + 16ac054)

**事故**:部署 f78e88b 后,LLM_REVIEW_FAILED 落库与推送器 HasAction 查询全部失败——`validAction` 白名单与 DB CHECK 约束都缺这个新 disposition,747 推送链卡死(failed check 循环,监控捕获)。

- **2fee07c**:internal/wheelstore/store.go `validAction` 加 `LLM_REVIEW_FAILED`(Go 层校验)。
- **16ac054**:internal/db/migrations/012_wheel_action_llm_review_failed.sql 重建 `wheel_signal_actions_action_check` 约束加 `LLM_REVIEW_FAILED`(010 迁移建的约束不含,落库被 CHECK 拒绝)。

**验证**:迁移 012 已应用(pg_get_constraintdef 确认);747 推送链解除,telegram/discord 正常推送 REJECTED 卡片。

**教训**:新增 disposition/枚举必须同步 Go 校验 + DB CHECK 约束两处,verify.sh 全绿也测不到 DB 约束(测试用内存 fake repo);以后枚举变化加一条「DB 约束检查」清单项。

## Next / Known Limitations

- **孤儿信号重放未实现**:重启/部署杀在途审核导致的「无动作信号」不会自动补审补推(742 丢失,2026-08-13 晚再发 748:当晚两次连续部署 2fee07c→16ac054 间隔 ~2 分钟,第二次重建杀了 748 在途审核;748 与 749 间隔仅 1.3 分钟内容相同,749 卡片已覆盖,影响轻微)。自动重审需重建当时审核上下文(positions/cash/pending),用当前状态审旧信号有失真风险;且补推历史卡片有骚扰用户风险。如需,应做成「启动时扫描 <30min 无动作新鲜 ALERT,人工确认后重审」的小工具,需老板拍板。
- **部署纪律(2026-08-13 教训)**:连续部署(紧急修复)会杀在途审核。部署前应检查 DB 是否有「最近 5 分钟内 ALERT 且无 action 的信号」;有则等其审核完成再重建,或接受丢失并在汇报中说明。
- 推送器「not yet recorded」卡链的另一个触发面(审核正常但 LatestLLMReview 查不到)在 739 上出现过一次,本次未再复现,未追查到底;若再现需深查 store 查询语义。
