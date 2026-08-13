# 2026-08-12 Discord 智能助手(斜杠命令 /ask /a /b:双引擎问答 + 功能操作)

- **id**: `2026-08-12-discord-assistant`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(① Telegram 智能助手挂起,不开发不保兼容 ② Discord 成主渠道 ③ 智能助手转 Discord 支持 ④ 挂起 web 页面开发,全力支持移动端操作接口)

## Goal

在 Discord 里用斜杠命令对话:助手能回答问题(双引擎:Claude/Codex)并操作现有系统功能(策略/配置/回测/下单确认),成为移动端主操作入口。与现有按钮确认闭环共享交互基建。

## 决策(老板拍板 2026-08-12)

- **触发**:先支持 `/ask`;`/a` = ask Claude、`/b` = ask Codex(双引擎 CLI 子进程);后续不够用再加频道 @
- **会话**:每条消息独立上下文(无状态起步,便宜)
- **能力**:尽量覆盖现有功能——更新策略(watchlist/策略参数)、更新配置(admin 可写项)、显示回测结果、行情/策略问答、工程答疑
- **安全**:下单必须走现有按钮确认闭环(✅/❌ 按钮,不变);涉及钱的命令一律不自动执行
- **白名单**:只响应老板(沿用 allowed_user_ids 思路,与现有交互一致)
- **挂起**:Telegram(不开发不保兼容)、React web 重构 P1-P4(资源转 Discord)
- **战略放弃(2026-08-12 老板重申)**:web 工具暂时战略放弃,全面偏向 Discord 移动端开发;React 重构整体挂起

## Discord 移动端能力盘点(2026-08-12 老板问「Discord 是否支持小程序」)

- **结论:Discord 没有真正的小程序;最接近的 Activities(嵌入式 web 应用,Embedded App SDK)移动端支持不完整,不适合做主操作界面**
- 移动端完整可用的形态 = **消息驱动的交互**:Slash 命令 + Message Components(按钮[现有 ✅/❌ 闭环]/Select 菜单/Modal 弹窗表单)+ Embed 卡片 + 图片附件(图表转图)+ Thread/Forum 频道组织
- 技术路线:**不引入 Activities 容器**,移动端操作面全部基于消息组件;行情图等可视化走「生成图片发附件」(后续切片)

## 范围(拟切片,待细化)

1. **切片 1:斜杠命令框架**(约 1-1.5 人天)
   - interactions 端点识别 CHAT_INPUT 命令(/ask /a /b)+ 参数(问题文本)
   - interaction 3 秒超时 → DEFERRED_CHANNEL_MESSAGE + followup 异步回复
   - CLI 子进程执行器:Claude(`claude -p`)与 Codex(`codex exec`)**双引擎抽象**(接口:Ask(ctx, prompt) (string, error)),各自超时/失败/额度(usage limit 退回另一引擎?)策略
   - 白名单 user id 校验
2. **切片 2:功能操作路由**(约 1-2 人天,依赖切片 1 或并行)
   - 意图识别 → 调用现有内部逻辑(策略更新/配置写/回测查询),不重复实现
   - 只读 vs 写操作区分;写操作输出确认要求(涉及钱的必须按钮确认)
   - **「再来一单」(老板拍板 2026-08-12)**:用户对助手说「给 XX 再来一单」→ 助手触发该 symbol **立即重新评估** → 走完整管道:评估 → LLM 审核(**新的风险评估**)→ 推送新卡片(**新的参数/价格/限价**,新 ID、新 5 分钟窗口)→ 用户在新卡片上按钮确认。助手不代替确认,确认永远走按钮闭环;无新信号则回复「当前无新信号」
4. **切片 4:交互输入全量日志 + 需求自动沉淀(2026-08-12 老板指令:「机器端输入都留日志,作为后续功能开发参考;没实现的需求自动排入需求队列」)**
   - **落库**:新表 `interaction_log`(user_id/channel_id/interaction_type/command/raw_input/handled/unfulfilled/created_at),serve 交互端点每条交互(按钮/命令/消息/被拒交互)全量记录——**数据从即日起积累**(现有按钮确认闭环就开始记录,不等 /ask)
   - **未实现打标**:`unfulfilled=true` 判定——未注册命令、被拒交互、/ask 未能回答的问题、语义上「用户要了但系统没有」的输入;已支持交互(确认/否决/已实现命令)标记 handled
   - **自动排需求**:定期聚合 unfulfilled 日志(LLM 分析或规则聚合)→ 去重归并 → 生成需求候选(需求队列,owner 接单)→ 老板可查「用户想要什么但还没有」
   - 查询入口:serve 端点或 CLI(如 GET /v1/discord/interaction-log?unfulfilled=1)+ 审计视图(Discord 内或日志输出)
   - **可与切片 1 并行**:日志层无 /ask 依赖,交互基建之上直接加;越早开始积累样本越多
3. **切片 3:部署形态**
   - CLI 子进程在 serve 容器内(装 claude/codex + 凭据走 ~/.wbot/,0600)vs 宿主中转——待定,倾向容器内
   - 交互端点公网已通(UA 放行 + 验签),无网络前置

## Constraints

- CustomID/按钮闭环不变;Telegram 路径不动(不开发,运行现状保持)
- 敏感凭据只进 ~/.wbot/;测试 fixture 假值;verify.sh 全绿才提交
- 提交署名按实际编写模型(codex 署 gpt-5.6-luna)
- codex 单飞纪律:切片内 codex CLI 调用是「助手功能」不是开发派单,不冲突;但同一时刻用户手动派 codex 时助手调 codex 需排队/降级(设计时考虑并发互斥)

## Links

- 交互基建:#39(done,interactions 端点 + 验签 + 按钮闭环)+ 2026-08-12-discord-push-ui(推送 UI 重排版,进行中)
- 原 #31(Telegram 智能助手):挂起,方向并入本任务
- 功能面:策略 CRUD(现有 API)、配置写(admin)、回测(现有端点)

## State

- **status**: `in_progress`(MVP 代码与本地验收完成;待部署真机验收)
- **last step**: 2026-08-13 完成 `/ask` 全局命令启动注册、type 2 deferred→Claude CLI(120s)→原消息 PATCH、空白名单放行 backlog 日志/已配白名单拒绝、1900 字符截断、容器 Claude runtime 与假 CLI/handler/幂等注册测试；`scripts/verify.sh` 全绿

## Next

- 部署当前 feature 后真机 `/ask "你好"` + `/ask "当前 00700 信号状态"` 验收；确认稳定后将 `assistant.discord.allowed_user_ids` 配为老板 Discord user id(空值当前按 MVP 约定放行并记录 backlog 日志)
- 后续切片再做 Codex `/b`、功能操作路由、连续对话与交互日志；现有按钮闭环保持不变

## MVP 切片(2026-08-12 老板指令:「先实现我说话能够回答我,后续再迭代」)

**目标(最小可用)**:Discord 里 `/ask <问题>` → serve 收到 → 拉起 Claude CLI headless 回答 → 回复到频道。能说话能回答即验收,双引擎(/b codex)/功能操作/连续对话全部后续迭代。

### 范围(MVP)

1. **命令注册**:`/ask`(参数 question,必填;描述中文)——注册用 Discord API `PUT /applications/{app_id}/commands`(global);注册工具或 serve 启动时注册(二选一,建议启动时幂等注册)
2. **interactions 处理**:handleInteraction 加 type 2(APPLICATION_COMMAND)分支:校验 command name=/ask → 白名单校验(user id,沿用 allowed_user_ids 思路;未配白名单时默认只放行 owner——或先放行全部?→ 默认白名单为空则仅记录日志放行,标注 backlog 收紧)→ 立即回 `{type:5}`(DEFERRED_CHANNEL_MESSAGE_WITH_SOURCE)→ 后台执行
3. **CLI 执行器**:`Ask(ctx, prompt) (reply string, err error)` 抽象(先只实现 Claude:`claude -p "..."` 子进程);超时 120s;回答 >1900 字符截断+提示(超过 Discord 消息限制);失败回复「调用失败+原因」
4. **回复**:followup `POST /webhooks/{app_id}/{interaction_token}` 或 PATCH messages/@original;internal/discord 包加 followup/注册命令方法
5. **部署**:serve 容器 runtime 加 node + claude CLI(容器内安装;ANTHROPIC_API_KEY 走 ~/.wbot/ 挂载 env,0600);serve.env 加配置项(assistant.claude.api_key 或 env)
6. **测试**:executor 用假命令 fixture 测(注入假 Ask 实现);handler 单测覆盖 type2/白名单/deferred 时序;注册幂等性测试
7. **验收**:verify.sh 全绿 → 部署 → 真机 /ask "你好" 有回复;再问一个真实问题(如「当前 00700 信号状态」)验证回答质量

### 排期与依赖

- 依赖:2026-08-12-discord-push-ui 收口合入(同文件 cmd/wbot/discord_scheduler.go 的 handleInteraction,串行)
- 派单:codex(gpt-5.6-luna),worktree 独立分支
- 成本:claude CLI 按量 API key(非订阅)受高峰时段约束;codex CLI 走订阅不受限——MVP 先 Claude 单引擎
