# 闭环: 限频池跨进程聚合 (FUTU_RATELIMIT_DIR)

- **日期**: 2026-08-03
- **PR**: #301(本体)+ 本文档(归档)
- **背景**: FUTU.md §8 排期项「shell 循环反复启动 wbot 会绕过限频池」——跨进程聚合;候选池枯竭后按排期项扫描命中。

## 改动

- `internal/futu/ratelimit.go`: 包级限频器(QuoteLimit/KlineLimit/HistoryPageLimit/SnapshotLimit)改经 `persistedLimiter` 构造——设置 `FUTU_RATELIMIT_DIR` 后各档位共享 flock 时间戳文件(`<dir>/<tier>.ts`,UnixNano);单 flock 会话内完成读-决策-标记(无竞态);文件不可写自动降级纯内存(请求永不失败);env 未设置行为完全不变
- `internal/futu/ratelimit_test.go` +4 测试: 跨实例共享节奏(两 Limiter 同文件 8 goroutine 间隔断言)/ **真跨进程**门控(re-exec 子进程,父进程盖章后子进程首过等待 ~200ms,锁定「反复启动 wbot」场景)/ 不可写降级 / env 开关接线
- `doc/FUTU.md` §8: 生效范围段更新(默认进程内;`export FUTU_RATELIMIT_DIR=~/.wbot/ratelimit` opt-in 跨进程)
- `doc/API.md`: options 示例补 premium_close/premium_close_ts 展示(call 行有值、put 行缺省,P3a 遗留)

## 验证

- verify.sh 连跑两遍全绿(含 CLI smoke);CI 5/5(含 db-integration 真 PG)
- 真跨进程测试: TestLimiterCrossProcess re-exec 子进程 0.41s(父盖章 → 子门控 ~200ms)
- 本地部署: serve:8080 `/v1/health` ok、`/v1/futu/options` 真网关响应正常

## 备注

- **设计决策(显式开启)**: 跨进程聚合做成 `FUTU_RATELIMIT_DIR` opt-in 而非默认——避免测试进程被包级限频器拖慢(3s 档位)与未预料的文件系统副作用;未设 env 的 CI/本地行为零变化。
- **flock 单会话**: read→decide→mark 必须在一个 flock 会话内完成,否则两进程都读旧章都通过(竞态);锁序 l.mu → flock 单向,无死锁。
- **降级语义**: 文件不可写时 Wait 不失败、纯内存节奏继续——限频是保护措施,不是硬依赖。
- 剩余排期: 实时逐合约 option-quote/IV 填充(P3b)、多 symbol 时间对齐(待拍板)。
