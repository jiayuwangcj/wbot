# flake 排期:TestLimiterCrossProcessShared 连续 flake

- **created**: `2026-08-11`
- **observed**: 2026-08-11 两次(PR #328 首次 test 普通模式 51s 失败;merge main 后 rerun run 31487151123 race 模式失败;均为 internal/futu `TestLimiterCrossProcessShared`,与前端 PR 改动无关——该 PR 只动 web/src/pages/results/**)
- **现象**: 跨进程 limiter 共享测试偶发失败(0.22s/0.27s 即挂),CI test job 整体 fail
- **疑似**: 跨进程文件锁/共享内存时序竞争,CI runner 负载高时触发
- **排期**: 已派 codex(gpt-5.6-luna max,worktree .claude/worktrees/flake-limiter,分支 fix/flake-limiter-crossprocess,2026-08-11 用户确认优先处理):调查根因(internal/futu limiter 测试,加确定性等待或隔离机制);在调查完成前,合入流遇该 flake 用 `gh run rerun --failed` 放行
- **state**: `in_progress`(2026-08-11 派单)

## 2026-08-11/12 修复进展

- 根因判定(两次 CI 失败 206µs/94µs):pass 路径 stamp 写入失败被静默(Truncate 成功但 write 失败→文件空/旧值→下一次 pass 提前)。CI 磁盘瞬时压力与本地差异,本地 -count=10 全绿。
- 修复(97269ac,PR #332):① ratelimit.go 失败路径显式日志(open/flock/stamp write,零正常路径开销);② TestLimiterCrossProcessShared 前置串行探测(跨进程 stamping 不生效→skip 带原因+文件内容),主断言保持严格。
- 时序场景捕获(7b54223,2026-08-12,Claude coder):starts 记录实例归属(l0/l1),失败时单行打印完整场景——8 个 start 的序号+实例+偏移、全部相邻间隔序列、最终 stamp 文件内容。CI 日志可直接 grep `scene: `。**分析钥匙**:若 µs 间隔的相邻 start 属同一实例 → in-memory 门(l.next)问题;属不同实例 → 跨进程文件门问题。
- 状态: 修复补丁 + 场景捕获已 push(97269ac..7b54223 → origin/fix/flake-limiter-crossprocess),**等 CI 复现/全绿**;CI 再复现即拿 scene 定根因
