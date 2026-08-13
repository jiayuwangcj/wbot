# 2026-08-12 监控独立化(监控常驻,独立于业务与会话稳定运行)

- **id**: `2026-08-12-monitor-independence`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(「我老看见你说监控挂了,后续查明原因并且优化,我理解监控应该尽量独立于业务稳定运行」)

## Goal

serve 的业务监控**不依赖 Claude 会话/手动挂命令**,落成常驻、独立于 serve 业务的监控载体;监控自身故障可被发现,不拖垮业务。

## 根因(已查明)

1. **唯一实时监控是会话内 Monitor**(`docker compose logs -f serve | grep`):会话结束/上下文压缩即消失——「老挂」的本质
2. **serve 容器无 docker healthcheck**(postgres 有,serve 没有):容器存活/健康无自动探测
3. **查询类命令易漏 `--env-file`** → compose interpolation 报错(本会话已发生两次),误读为「监控挂了」
4. **无业务级监控**:wheel 心跳(多久没新信号)、LLM 审核结果、Discord 推送成败均无常驻观察
5. 探活纪律已固化(memory serve-troubleshoot-discipline):只准 HTTP/日志探活,SIGQUIT 会杀 Go 进程——优化方案必须遵守

## 设计(拟)

1. **serve 容器 healthcheck**(compose 服务级,docker 原生):`curl -sf http://127.0.0.1:8080/v1/health` 定期探测,unhealthy 触发 restart_policy 或告警——随 compose 生命周期,独立于会话
2. **独立监控容器/脚本(wbot-monitor,compose 加服务或宿主 cron 常驻)**:
   - 进程/健康层:HTTP /v1/health 轮询(只 HTTP,不 SIGQUIT)
   - 业务层:wheel 心跳——wheel_signals 最新 created_at 距今超阈值(如 >2×pass 周期)→ 告警「runner 疑似卡死」;LLM 审核拒绝率异常;Discord 推送失败计数
   - 告警:异常 → 日志(带模块前缀可 grep)+ 复用 Discord 通知渠道(与推送同通道,老板手机可见)
3. **会话内 Monitor 降级为辅助**:仅用于「会话内临时盯一次推送」,长期监控归常驻载体
4. **查询命令纪律**:compose 查询统一带 `--env-file ~/.wbot/serve.env`(脚本化避免手误);或 serve-env.sh 输出 shell 别名/wrapper

## Constraints

- 探活只用 HTTP/日志,不 SIGQUIT/杀进程(固化纪律)
- 监控进程故障不影响 serve 业务(独立容器/独立 cron);监控自身失败要显式可发现(日志/告警)
- 监控容器不加鉴权面暴露风险;凭据走 ~/.wbot/(0600)
- 与进行中任务无文件重叠(wheel-inventory/push-ui 已合入,本任务新加 compose service + scripts/ + 可选 cmd);verify.sh 相关保持
- 排期:不阻塞当前 US.JD 实测主线;完成后并入合批

## Links

- memory serve-troubleshoot-discipline(探活纪律:只 HTTP/日志)
- configs/docker-compose.serve.yml(serve 服务,无 healthcheck)
- internal/httpapi/httpapi.go:216(/v1/health 端点)
- 宿主 cron:44 8 * * * inspect.sh(每日巡检,可参考形态)

## State

- **status**: `draft`(2026-08-12 老板指令 → 记录创建)
- **last step**: 根因盘点 + 设计拟;待细化派单

## Next

- 细化设计(healthcheck 参数/监控项阈值/告警渠道)→ 排入队列(当前:库存修复已合入,LLM 策略落地 #37 优先)→ 派单
- 快速先做项:serve healthcheck 一行改动即可先上(不依赖派单)
