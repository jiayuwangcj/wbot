# 2026-08-12 Discord 通道接入(单向通知 + 交互面板闭环)

- **id**: `2026-08-12-discord-channel`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(Telegram 推送需打开 app 才收到 → 评估接入 Discord;实测 discord.com 可达;面板控件评估确认)

## Goal

Discord 作为第二条推送/确认通道:信号/订单/拒绝理由通知 + embed 卡片 + 按钮确认闭环(✅下单 / ❌拒绝),与 Telegram 并行;公网入口 `https://w-cloud.top:1443`(haproxy → 内网)。

## 网络与基础设施(已实测确认)

- 公网: `https://w-cloud.top:1443`(TLS 1.3,ZeroSSL 证书至 2026-09-12,`curl -v` 验证通过)
- haproxy 转发目标(内网): **`192.168.139.2:8080`**(OrbStack host 网络容器 eth0 IP;VM 宿主 192.168.139.147 上无监听——host 网络非真正共享栈)
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

- **status**: `reviewing`(P1 修复中)
- **last step**: 2026-08-12 reviewer 评审 4b7d8c7 → **有条件合入**(feature):① P1 CONFIRM 并发竞态(双击/双通道同时确认 → 同信号两张限价单,HasAction↔AppendAction 间隔着 PlaceOrder)建议修后合入——已派 coder 修复(mutex 串行化 + 并发测试);② P2 doc/API.md 补契约、P2 用户白名单 allowed_user_ids、P3 若干 → 记 backlog 合入后跟进
- 安全总评:Ed25519 验签正确(±5min 防重放含未来时间戳、签名不过一律 401、错误文案不泄信息)、伪造/重放均被阻断;公网仅 /v1/discord/interactions 且只 POST

## Next

- coder 修 P1(mutex + 并发测试)→ verify 全绿 → reviewer 复核 → 合入
- backlog:wheel_signal_actions 部分唯一索引 (signal_id) WHERE action='CONFIRM'(双通道兜底)、doc/API.md 契约、allowed_user_ids 白名单、embed 发送失败重试、公网端点限速
- 晚上老板提供 Public Key / Bot Token / channel_id / Interaction URL 配置 → `tools/tg-test -discord` 实测 → 公网→内网连通 → Discord 面板验收
