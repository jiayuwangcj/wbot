# option-quote envelope err_code 类型修复(00700 明早实测阻断)

- **id**: `2026-08-12-option-quote-envelope`
- **created**: `2026-08-12`
- **updated**: `2026-08-12`

## Goal

修复 futu gateway option-quote 端点响应解析 bug——00700 DATA_BLOCKED 的根因,明早 9:30 实测的阻断项。

## Bug 现场(serve v0.2.0 实测日志)

```
futu: option-quotes: greeks HK.TCH260821C380000: option-quote: bad JSON: json: cannot unmarshal number into Go struct field envelope.err_code of type string
```

## 根因

- `internal/futu/client.go:51` `envelope.ErrCode string json:"err_code"`——**类型断言错误**:`/api/quote` 等端点的 err_code 是 string,但 `/api/option-quote` 端点返回 number(0)
- json.Unmarshal 失败 → post 返回错误 → option-quote 全挂 → greeks 缺失 → Bid/Ask=0 → wheel.Validate 拒 → 无 ALERT → 00700 DATA_BLOCKED
- ErrCode 字段当前无任何读取点(post 只检查 RetType != 0),纯类型兼容修复

## Constraints

- 修复方案:`ErrCode` 改 `json.RawMessage`(或 any),注释说明 gateway 不同端点 err_code 类型不一(number/string)
- 不碰其他文件(option_quote.go 无需改,解析层修好即通);internal/wheel/*、internal/wheelrun/*、cmd/wbot/* 一律不动
- 补 envelope 解析测试:模拟 err_code 为 number 和 string 两种形态
- verify.sh 全绿;提交到 `fix/option-quote-envelope` 分支

## Links

- 上游: doc/tasks/2026-08-11-wheel-data-link.md(数据链路已合入,此 bug 在其后实测暴露)
- Branch: `fix/option-quote-envelope`(worktree `.claude/worktrees/option-quote-envelope`,基于 origin/main 1d257d4)
- 执行者: codex gpt-5.6-luna(2026-08-12 派单;额度尽时退回 Claude coder)

## State

- **status**: `in_progress`(2026-08-12 派单 codex,修复中)

## Next

- codex 完成 → reviewer 评审(bugfix 属性,可及时发)→ PR → 合入 main → republish v0.2.0 → 重启 serve → 验证 00700 greeks 链路(不再 DATA_BLOCKED)
- 9:30 开盘实测衔接
