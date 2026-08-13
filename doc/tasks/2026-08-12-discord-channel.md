# 2026-08-12 Discord 通道接入(单向通知 + 交互面板闭环)

- **id**: `2026-08-12-discord-channel`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(Telegram 推送需打开 app 才收到 → 评估接入 Discord;实测 discord.com 可达;面板控件评估确认)

## Goal

Discord 作为第二条推送/确认通道:信号/订单/拒绝理由通知 + embed 卡片 + 按钮确认闭环(✅下单 / ❌拒绝),与 Telegram 并行;公网入口 `https://w-cloud.top:1443`(haproxy → 内网)。

## 网络与基础设施(已实测确认)

- 公网: **`https://tradedev.w-cloud.top:1443`**(TLS 1.3,ZeroSSL;Discord 注册的 Interactions Endpoint URL = `https://tradedev.w-cloud.top:1443/v1/discord/interactions`;⚠️ 旧文档的 `w-cloud.top` 为初始域名,已弃用)
- **UA 过滤**:公网入口只放行 Discord User-Agent(`DiscordBot (https://discord.com, v1.0)`),curl 测试必须 `-A` 带 UA,否则被拒(表现为连接超时/503)
- haproxy 转发目标(内网): **`192.168.20.201:8080`**(serve 固定地址,与容器 eth0 漂移无关;旧文档的 192.168.139.2 已过时)
- 公网 IP `182.149.199.61` 为 haproxy 机器的宽带出口(与开发机 171.213.252.227 是不同宽带,各自独立;勿用开发机出口判断 DDNS 过期)
- serve 已监听 `0.0.0.0:8080`,无需改监听;容器重建后容器 IP 可能变化,需重新确认
- **Telegram setWebhook 不实施**(用户决策:443/80/88 高危,1443 不满足 Telegram 端口要求;后续测试加入 8443 再定)——Telegram 保持 getUpdates 轮询现状

## 范围(约 2-3 人天)

1. **internal/discord 包**:webhook 发送(embed + buttons)+ **Interaction 验证(Ed25519 签名 + X-Signature-Timestamp 防重放,必做——否则任何人可伪造按钮点击触发下单)** + 交互响应(PING→PONG / callback)
2. **serve 新端点**:`POST /v1/discord/interactions`(Discord 注册的 Interactions Endpoint URL = `https://w-cloud.top:1443/v1/discord/interactions`)
3. **推送双发**:信号/订单/拒绝理由推送并行发 Telegram + Discord(embed 卡片,状态色:ALERT 红 / APPROVE 绿 / 拒绝灰)
4. **确认闭环**:Discord 按钮回调 → 复用现有 telegram_scheduler 的信号处理逻辑(APPROVE/REJECT 语义一致,独立 handler 入口)
5. **配置**:wbot.conf `credentials.discord.app_id / public_key / bot_token`(0600,不进仓库)
6. **格式**:HTML → Discord Markdown 转换(粗体/换行/代码块)
7. **测试**:发送测试工具扩展(tg-test 加 `-discord` 模式)+ interaction 签名验证测试(假 public key fixture)

## Constraints

- **公网 1443 只暴露 `/v1/discord/interactions` 一个 POST 路径**——haproxy 按路径转发,serve 其余端点(UI/API/下单)绝不走公网;discord public_key 验证不过的请求一律 401
- 所有策略仅限价单;推送必须带信号/订单编号(双通道同规)
- 测试 fixture 假值;敏感配置只进 ~/.wbot/
- 行为零变化不适用于本任务(新功能);但不改 Telegram 现有路径行为
- 提交署名按实际编写模型;verify.sh 全绿才提交

## 前置/依赖

- 老板需在 Discord Developer Portal 创建 Application:获取 **Public Key + Bot Token**,并把 Interactions Endpoint URL 设为 `https://w-cloud.top:1443/v1/discord/interactions`(bot 需加入目标频道/服务器)
- haproxy 配置:w-cloud.top:1443 → 192.168.139.2:8080,仅转发 `/v1/discord/interactions`
- 与 #36(共享管道抽象)无文件重叠(新包 internal/discord + serve 路由注册点),可并行

## Links

- 评估:本任务记录(实测/方案);telegram 现有确认闭环 cmd/wbot/telegram_scheduler.go
- 后续切片(待 8443 就绪):Telegram setWebhook 切换(0.5-1 人天)

## State

- **status**: `done`(已合入 feat/llm-signal-endpoint)
- **last step**: 2026-08-12 全流程:实现 4b7d8c7 → 评审(**有条件合入**,feature;P1 CONFIRM 并发竞态)→ coder 修 c6495fa(confirmMu 串行化 + blockPlacer 并发回归测试,反向验证无锁时确定性双下单)→ 复核通过 → 合入 feat/llm-signal-endpoint(c51ca11);与 #36 合入后同步适配 3c285fa(wheelTelegramStore → wheelstore.SignalRepository、Candidates 强类型化,行为不变)
- 安全总评:Ed25519 验签正确(±5min 防重放含未来时间戳、签名不过一律 401、错误文案不泄信息)、伪造/重放均被阻断;公网仅 /v1/discord/interactions 且只 POST

## Next

- ✅ 按钮交互闭环已验证(2026-08-13,信号 500 CONFIRM+FILL),456 卡片按钮已超窗(signal expired 属预期);待 00700 实测数据积累后评估 wheel 策略有效性
- ✅ 按钮清理视觉验证(2026-08-13 02:18):456 卡片点 ❌ 拒绝 → DB 记录 NO「继续等待机会」(id=119,actor=`discord:1486343344065089648`)+ clearDiscordButtons PATCH 无失败日志(按钮消失);ephemeral 删除规则确认:仅 ✅ 路径的「已记录,正在下单」(in-progress)在异步结果后删除,❌ 路径「已记录,继续等待机会」为最终回复保留
- 注意:Discord 后台若改动 Interactions Endpoint URL,**必须配置在应用(General Information)页而非 Bot 页**,否则交互事件不达 endpoint
- CI(feat/llm-signal-endpoint,已 push)→ 合批发布(feature)
- backlog:wheel_signal_actions 部分唯一索引 (signal_id) WHERE action='CONFIRM'(Telegram×Discord 交叉确认兜底)、doc/API.md 补 /v1/discord/interactions 契约、allowed_user_ids 白名单(与 chat_ids 对齐)、embed 发送失败重试、公网端点限速
- 观测缺口(2026-08-13 监控发现):pushRejectedDiscord `_ = s.pushEmbedDiscord(...)` 吞错且无日志,拒绝推送成败 serve 侧不可见(telegram 有「pushing reasons」,APPROVE 路径有错误日志)——补日志/错误上报
- 澄清(2026-08-13):推送循环启动 cursor=MaxSignalID,重启**不会**重推已处理信号(454 于 00:21 重建后无重推实证);此前 453「重复 pushing reasons」为跨容器日志时序误读
- 现网观察(2026-08-13):US.JD signal=454 模型真实拒绝(cash_available null + quote 时间戳全零),与 429 同因——US.JD 模拟账户现金/行情数据缺口,LLM 连续 fail-closed
- 按钮交互排查与根因(2026-08-13):456 卡片「✅ 下单」点击两次(00:29 与上午)均 Discord 提示失败、serve 零 interaction 日志 → 逐层排查:内网 serve 健康(interactions 路由 401 正确)→ 公网链路用**正确域名 tradedev.w-cloud.top + UA=discord** 验证全通(401=达 serve 验签层)→ 排查中收到真实 Discord 请求 `signature mismatch`(01:45:01,用户 Save URL 时的 PING,证明请求可穿透 haproxy 达 serve)→ **根因:Interactions Endpoint URL 配置位置错误——之前配在 Bot 页,应配在应用 App/General Information 页**(配错位置 Discord 不向该 endpoint 发交互事件,表现为点击即失败+serve 零日志)→ 老板更正配置后**端到端闭环首次实测成功**:信号 500(HK.00700,PUT 缺口 300)01:45:33 收到真实交互(CONFIRM,actor=`discord:1486343344065089648`)→ 01:45:50 模拟盘 FILL 成交
- 排查工具沉淀:tg-test 新增 `-buttons -signal <id>` 模式(可推带 ✅/❌/⚠️ 按钮的测试卡片,与 serve 推送同构),供链路测试复用
- 晚上老板提供 Public Key / Bot Token / channel_id / Interaction URL 配置 → `tools/tg-test -discord` 实测 → 公网→内网连通 → Discord 面板验收
