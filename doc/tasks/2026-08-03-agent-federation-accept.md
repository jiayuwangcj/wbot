# 闭环 #41: agent 联邦端到端验收脚本

- **日期**: 2026-08-03
- **PR**: #202(脚本 + 归档合一)
- **背景**: 「验收覆盖扩展」引擎(同 #34/#35 引擎)对账发现 master/agent 子系统(ROADMAP 占位,保留作可测 smoke)验收覆盖为零——verify.sh/CI 只有 `master -duration 1ms` + `agent -duration 1ms -interval 1ms` 的启动冒烟,HTTP 契约(`/v1/register`、`/v1/agents`)与 agent→master 注册往返仅有单元测试,无端到端验收。恰逢 #40 为这两个端点补了文档——契约已有文字,缺实测背书。

## 改动

`scripts/accept-agent-federation.sh`(沿用 accept-* 模式,无 PG 依赖——in-memory 注册表):

- POST /v1/register: 首次 `{new:true}` / 重复 `{new:false}` / 第二个 id 登记
- GET /v1/agents 列出已登记 id
- 错误契约(纯文本,非 S5): 405 错方法 / 415(curl `-d` 默认 form 头)/ 400(JSON 头 + 坏体)/ 404 未知路径
- e2e: `wbot agent -id acc-e2e -master-url <addr> -duration 2s -interval 100ms` 跑完后自身出现在 /v1/agents(poll.Run 启动即注册,注册表无 TTL,退出后仍在)

## 验证

- 11/11 连跑两次稳定
- CI 5/5 全绿

## 备注

- **引擎经验**: 「验收覆盖扩展」的对象不仅是 serve 数据面——CI/verify.sh 里的「启动冒烟」不等于验收,凡有真实 HTTP/CLI 契约的子系统都要有自己的 accept 脚本;文档(#40)与验收脚本(#41)成对落地,契约先文字后实测。
- **候选池**: 仍枯竭。
