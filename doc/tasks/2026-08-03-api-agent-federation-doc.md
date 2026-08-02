# 闭环 #40: API.md 补 Agent federation(`wbot master`)文档

- **日期**: 2026-08-03
- **PR**: #200(文档单独 PR,无需归档 PR——功能即文档,归档即 PR 本身;另含 #198/#199 刷新按钮忙态本轮一并记录)
- **背景**: 「文档欠账对账」引擎发现**对账粒度盲区**——此前每轮只对账 serve mux vs API.md,本轮把范围扩到**二进制全部 HTTP 面**(grep 全部 internal/ 的 `"/v1/` 路径),发现 `/v1/register` + `/v1/agents` 由 `wbot master`(agent 联邦注册,ROADMAP 定位「可测占位与 CI smoke」)提供,**零现行文档**——仅存于 2026-04-17 任务归档(http-register-transport / master-optional-tls / v1-inprocess-poll-loop)。

## 改动

`doc/API.md`:

- 首段 scope 注明: 二进制另有一个独立 HTTP 面由 `wbot master` 提供,与 serve 数据面无交集
- 新增「Agent federation(`wbot master`)」章节:
  - `POST /v1/register`: body `{"id": string}`(Content-Type 需 application/json 或不设)→ 200 `{"new": bool}`(首次 true,重复 false);405 非 POST / 415 非 JSON 头 / 400 坏 JSON
  - `GET /v1/agents`: 200 `{"agents": [id...]}`;405 非 GET
  - 错误体为**纯文本** `http.Error`,**不接入** S5 统一契约(S5 仅覆盖 serve 数据面)
  - in-memory 注册表(master 重启即空);客户端对 503 重试(RetryMax 默认 0 不重试)、400 不重试
  - 附 curl 示例

## 验证

- 本地起 `wbot master -listen 127.0.0.1:8090` **逐条实测**契约: 首次 new=true / 重复 false / agents 列表 / 405 / 415(curl `-d` 默认 form Content-Type——示例必须显式 `-H 'Content-Type: application/json'`)/ 400「invalid json」(JSON 头 + 坏体)/ 空 Content-Type 接受(与「或不设」语义一致)/ 未知路径 404
- CI 5/5

## 备注

- **引擎经验**: 「文档欠账对账」的端点清单不能只 grep serve mux——要 grep **二进制全部 HTTP 面**(含独立子命令如 master);对账结果若为「范围外」也应落一行 scope 说明,防止后续轮次重复 flag 同一缺口。
- **示例可跑性**: 文档示例逐字验证时发现 curl `-d` 自动加 form Content-Type → 415,示例必须带 `-H` 头——「或验证、或改示例」,选了改示例。
- **候选池**: 仍枯竭;本轮同时完成 #39(刷新按钮忙态,PR #198+#199)。「UI 交互打磨」引擎候选已全部落地;下一步继续探索引擎或等待老板。
