# flake 排期:TestLimiterCrossProcessShared 连续 flake

- **created**: `2026-08-11`
- **observed**: 2026-08-11 两次(PR #328 首次 test 普通模式 51s 失败;merge main 后 rerun run 31487151123 race 模式失败;均为 internal/futu `TestLimiterCrossProcessShared`,与前端 PR 改动无关——该 PR 只动 web/src/pages/results/**)
- **现象**: 跨进程 limiter 共享测试偶发失败(0.22s/0.27s 即挂),CI test job 整体 fail
- **疑似**: 跨进程文件锁/共享内存时序竞争,CI runner 负载高时触发
- **排期**: 需要 coder 调查根因(internal/futu limiter 测试,加确定性等待或隔离机制);在调查完成前,合入流遇该 flake 用 `gh run rerun --failed` 放行
- **state**: `pending_investigation`
