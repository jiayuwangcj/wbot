# tasks

主对话**每次向 Subagent 发布可执行子任务**时，同步在此目录新增一条记录，便于上下文丢失后恢复。

在 **[[AUTO_ADVANCE]] 根任务循环**里，本目录属于 **「落盘」**：取任务时优先读此处 `status`（`running` / `queued`）；每小步结束更新 `updated`、State、`last step`、`Next`，使下一小步不依赖聊天上下文。

## 命名

`YYYY-MM-DD-<short-slug>.md`（slug 用小写连字符，见 [[_template]]）

## 恢复

1. 按日期或 slug 打开对应文件  
2. 将其中 **Goal / Constraints / Links / State** 整段复制到新会话作为干净上下文

## 注意

- 勿写入密钥、token、完整 PAT  
- 与 GitHub 驱动协作时，**Links** 里贴 `Driven-By` 或触发留言 URL

关联：[[WORKFLOW]] [[WORKFLOW_GITHUB_DRIVEN]] [[README]]
