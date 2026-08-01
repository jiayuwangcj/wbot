# Futu 网关部署与 `wbot futu` 客户端（⑪-a/b）

富途接入的**连接层**：网关以容器方式跑在本地，⑪-b 的 `wbot futu` 子命令经 **22222 端口 REST API**（futu-opend-rs，标准库 net/http，零新依赖）连接；11111 端口 protobuf API 保留给 proto 客户端。对应 `doc/tasks/2026-07-31-futu-integration.md` ⑪-a/⑪-b，行情落库见 [[DATA_PIPELINE]]。

## 前置条件

- Docker + Compose v2（`docker compose version` 可跑）
- 富途账号 + 密码 + 首登短信验证码（老板侧，见 [[ORGS]]）
- 敏感值只放 `~/.wbot/config.yaml`（[[PRIVACY]] 红线：仓库与镜像配置**零凭证值**）
- Go toolchain（`tools/config-to-env.sh` 内部 `go build` 渲染；构建过 wbot 的机器都有）

## 1. 凭证注入（~/.wbot/config.yaml）

部署级配置统一写 `~/.wbot/config.yaml`（YAML；`${VAR}` 展开为宿主环境变量，未定义 → 报错含变量名；`${VAR:-default}` 给默认值）：

```bash
mkdir -p ~/.wbot
cp tools/config.yaml.example ~/.wbot/config.yaml
chmod 600 ~/.wbot/config.yaml    # 仅本人可读（渲染工具校验，不通过即报错）
# 编辑：futu.login_account 等值用 ${VAR} 引用环境变量，或 ${VAR:-默认值} 就地占位
```

示例形态：

```yaml
futu:
  login_account: "${FUTU_LOGIN_ACCOUNT}"        # 值来自环境变量；未定义即报错（fail-fast）
  login_pwd_md5: "${FUTU_LOGIN_PWD_MD5}"        # MD5 生成见下方要点
  login_region: "${FUTU_LOGIN_REGION:-sh}"      # 可选：sh=上海(默认) | hk | us
```

渲染为 dotenv 再交给 compose（`wbot configyaml` 输出纯 `KEY=VALUE` 行，经 `tools/config-to-env.sh` 薄封装）：

```bash
umask 077    # 或渲染后 chmod 600 ~/.wbot/.env：派生文件含凭证，> 按 umask 落盘（默认 0644 过宽）
tools/config-to-env.sh ~/.wbot/config.yaml > ~/.wbot/.env    # --env-file 格式（无 export 前缀）
docker compose --env-file ~/.wbot/.env -f configs/docker-compose.futu.yml up -d
```

要点：

- MD5 必须对密码明文计算且**不含换行**：`echo -n '<明文>' | md5sum`（`echo` 漏写 `-n` 会算出错误散列）；MD5 本身等同凭证，同样只进 config.yaml / 环境变量
- 派生 env 文件 `~/.wbot/.env` 含展开后的凭证值（如 MD5），权限须 600：渲染前 `umask 077`，或渲染后 `chmod 600 ~/.wbot/.env`
- 任何 compose 操作前先渲染 env 文件（`FUTU_LOGIN_REGION` 不设时默认 `sh`）

## 2. 拉取与启动

```bash
umask 077 && tools/config-to-env.sh ~/.wbot/config.yaml > ~/.wbot/.env   # 派生文件含凭证，落盘即 600
docker compose --env-file ~/.wbot/.env -f configs/docker-compose.futu.yml pull     # 首次拉取 ostai/futuopend:9.4.5418
docker compose --env-file ~/.wbot/.env -f configs/docker-compose.futu.yml up -d    # 后台启动
docker compose --env-file ~/.wbot/.env -f configs/docker-compose.futu.yml logs -f futu-opend
```

| 端口 | 用途 |
| --- | --- |
| `11111` | OpenD protobuf API（proto 客户端连接口） |
| `8000` | 首登短信验证码 WebSocket（见下节） |
| `22222` | telnet 控制台（compose 中默认注释，调试可放开）；**futu-opend-rs 网关的 22222 为 REST API**（见下节） |

登录会话与数据持久化在宿主 `~/.com.futunn.FutuOpenD`，重建容器不丢。

## 3. 首登短信验证码（8000 WebSocket）

首次登录（或验证码失效）时 OpenD 需要短信验证码：连接 `ws://127.0.0.1:8000`，收到下行 `{"type":"REQUEST_CODE"}` 后回复上行 `{"type":"VERIFY_CODE","code":"123456"}` 即可。

消息一览（JSON）：

| 方向 | 消息 |
| --- | --- |
| 下行 | `{"type":"REQUEST_CODE"}` 需要验证码；`{"type":"STATUS","status":N}` 当前状态；`{"type":"CONNECTED"}` 已连接；`{"type":"CLOSED"}` 子进程退出 |
| 上行 | `{"type":"VERIFY_CODE","code":"..."}` 提交验证码；`{"type":"STATUS"}` 查询状态；`{"type":"INIT"}` 触发初始化（仅 `FUTU_INIT_ON_START=no` 时有效） |

任意 WebSocket 客户端均可（websocat/wscat/浏览器 console）。python3 回填示例（需 `pip install websocket-client`）：

```python
import websocket
ws = websocket.create_connection("ws://127.0.0.1:8000")
for _ in range(60):
    msg = ws.recv()
    print(msg)
    if '"REQUEST_CODE"' in msg:
        code = input("短信验证码: ").strip()
        ws.send('{"type":"VERIFY_CODE","code":"%s"}' % code)
```

## 4. 无账号时的编排校验（`docker compose config`）

没有富途账号也能验证 compose 文件与注入机制，两态都该跑：

```bash
# 状态一：未渲染 env → 期望显式报错（`${VAR:?}` 生效即证明注入机制正确）
docker compose -f configs/docker-compose.futu.yml config
# => Error: required variable FUTU_LOGIN_ACCOUNT is missing a value: ...

# 状态二：已渲染 env 文件（值可为占位，无需真实凭证）→ 正常渲染
umask 077 && tools/config-to-env.sh ~/.wbot/config.yaml > ~/.wbot/.env
docker compose --env-file ~/.wbot/.env -f configs/docker-compose.futu.yml config
```

`tools/config-to-env.sh` 输出的 env 文件即 compose 要求的纯 `VAR=value` 行（无 `export` 前缀），可直接复用；不写 `--env-file` 时 compose 读宿主 shell 环境变量（即状态一）。

## 5. 常见错误

| 现象 | 原因与处理 |
| --- | --- |
| `required variable FUTU_LOGIN_ACCOUNT is missing a value` | env 未注入：先渲染 `tools/config-to-env.sh > ~/.wbot/.env` 并加 `--env-file ~/.wbot/.env` |
| `environment variable not set`（含变量名） | config.yaml 的 `${VAR}` 引用未定义：设置对应环境变量，或改 `${VAR:-默认值}` |
| 登录失败 / 账号或密码错误 | MD5 算错（换行、大小写、空格）：按第 1 节重算 |
| 验证码错误或过期 | 重连 8000 重新提交新短信验证码 |
| 行情报未开通市场（美股/港股等） | 账号未开通对应市场权限，**属预期错误**；向老板确认开通（`doc/tasks/2026-07-31-futu-integration.md` ⑪-c 文档注明） |
| 容器反复重启 | `docker compose logs futu-opend` 看日志；确认宿主 `~/.com.futunn.FutuOpenD` 目录可写 |
| 11111/8000 端口被占 | `ss -tlnp` 后过滤 11111/8000，定位占用进程 |

## 6. 连通性验证（⑪-b 前置）

```bash
nc -zv 127.0.0.1 11111    # API 端口可达即网关就绪
```

安全提醒：本文件与 compose 只含变量名与 `${VAR:?}` 占位，零凭证值；真实值仅存 `~/.wbot/config.yaml`（600 权限）与宿主环境变量，永不入 commit/PR。关联 [[PRIVACY]]。

## 7. `wbot futu` REST 客户端（⑪-b，2026-08-01 实测）

`wbot futu` 经 futu-opend-rs 网关的 **REST 22222 通道**取行情（legacy 模式读端点免认证，无凭证值；scope 模式需 Bearer header 时再扩展）。实现见 `internal/futu`（客户端）+ `cmd/wbot/futu.go`（CLI，Go 标准库）。

```bash
wbot futu status [-addr http://127.0.0.1:22222]            # 健康 + 登录态
wbot futu quote -symbol HK.00700 [-addr http://127.0.0.1:22222]   # 订阅后取 Basic 行情
```

实测输出（2026-08-01，futu-opend-rs 1.5.0）：

```json
$ wbot futu status
{
  "addr": "http://127.0.0.1:22222",
  "health": "ok",
  "server_ver": 1002,
  "qot_logined": true,
  "trd_logined": true,
  "time": 1785554473
}
```

```json
$ wbot futu quote -symbol HK.00700
{
  "basic_qot_list": [
    {
      "amplitude": 3.773,
      "cur_price": 475.2,
      "high_price": 479.8,
      "low_price": 462.0,
      "name": "TENCENT",
      "open_price": 470.0,
      "security": {"code": "00700", "market": 1},
      "update_time": "2026-07-31 16:07:51",
      "volume": 31100240
    }
  ]
}
```

**实测确认的 REST 路径**（与官方文档的出入已注明）：

| 动作 | 请求 | 说明 |
| --- | --- | --- |
| 健康检查 | `GET /health` | 200 `ok`；后端断开时 503（`{"error": ...}`） |
| 网关状态 | `GET /api/global-state` | envelope `ret_type=0`，s2c 含 `server_ver`/`qot_logined`/`trd_logined`/`time` |
| 订阅 | `POST /api/subscribe` | body `{"symbols":["HK.00700"],"sub_types":[1],"is_sub_or_un_sub":true}` |
| 行情 | `POST /api/quote` | body `{"security_list":[{"market":1,"code":"00700"}]}` |

出入记录：REST 参考文档（futuapi.com/en/reference/rest-api/）给出的 `{"symbol":"HK.00700"}` 与 `{"symbol":"HK.00700","sub_types":...}` 形态**实测均被 400 拒绝**（严格校验 v1.4.93 BUG-002：`unknown field 'symbol'` / `sub_type_list`）；有效 body 是 `security_list` 对象数组（market 枚举：1=HK、11=US、21=SH、22=SZ）。`quote` 命令先订阅后取数（未订阅时网关返回业务错误「未订阅以下股票」，属预期）；`wbot futu quote` 内部自动处理订阅，重复执行幂等。

**错误处理**：网关不可达 → `futu: status: ... connection refused`（exit 1）；HTTP 4xx/5xx → 输出状态码与 `{"error": ...}` 内容；业务错误（`ret_type != 0`）→ 输出 `ret_msg`。market 前缀支持 `HK./US./SH./SZ.`，其余报错（exit 2）。

网关地址暂用 `-addr` flag（默认 `http://127.0.0.1:22222`）；config.yaml 的 `futu` 配置接入待后续切片。行情落库管道见 [[DATA_PIPELINE]] ⑪-c。

## 8. `wbot ingest futu`：K 线落库（⑪-c，2026-08-01 实测）

`wbot ingest futu` 经 REST 22222 拉取 K 线写入 bars 表，复用 `RunIngestion` / `RunEveryResilient` 管道（ON CONFLICT 幂等、-every 调度韧性，见 [[DATA_PIPELINE]]）：

```bash
wbot ingest futu -symbol HK.00700 -timeframe K_DAY [-addr http://127.0.0.1:22222] [-from -to] [-every] [-dry-run]
wbot ingest futu -symbol HK.00700 -timeframe K_1M -from 2026-07-30T00:00:00Z -to 2026-07-31T23:59:59Z   # 分钟线建议显式范围
```

- `-timeframe` 用 futu 名称：`K_1M K_5M K_15M K_30M K_60M K_DAY K_WEEK K_MONTH`（ingest 名称 `1m 5m 15m 30m 60m 1d 1w 1mo` 亦可），落库用 ingest 约定（`bars.timeframe`）
- `-from`/`-to` RFC3339（空 from = 2004 级全量历史，空 to = now+24h 含当日未收盘 bar）；`-dry-run` 只拉取并打印条数与首末时间，不碰数据库
- `-every` 重复执行计入同一限频池；未收盘 bar 为部分数据，重拉因 ON CONFLICT 不覆盖（intraday 滚动更新时注意）

### K 线 REST 契约（实测路径）

| 动作 | 请求 | 说明 |
| --- | --- | --- |
| 订阅 K 线 | `POST /api/subscribe` | `{"symbols":["HK.00700"],"sub_types":[N],"is_sub_or_un_sub":true}`，N=6/11/7/8/9/12/13/16（Day/1Min/5Min/15Min/30Min/Week/Month/Year） |
| 最新 K 线 | `POST /api/kline` | `{"security":{"market":1,"code":"00700"},"kl_type":2,"req_num":100,"rehab_type":0}`；**需先订阅**，最多 1000 根，**拒绝** begin/end |
| 历史 K 线 | `POST /api/history-kline` | `{"security":{...},"kl_type":2,"rehab_type":0,"begin_time":"2026-07-20 00:00:00","end_time":"...","max_count":1000,"next_req_key":[...]?}`；**免订阅**、begin/end 必填、max_count≤1000 分页（游标回传 `next_req_key`，返回 `null` 即末页） |

**kl_type 枚举（实测，与文档/官方 proto 的出入已注明）**：`1=1Min 2=Day 3=Week 4=Month 5=Year 6=5Min 7=15Min 8=30Min 9=60Min`；复权 `rehab_type=0`（不复权）。`ingest futu` 只用 history-kline（免订阅、范围+分页一步到位）。

**响应字段（s2c）**：`kl_list[]` 每根含 `time`（网关本地 +08 墙钟）、`timestamp`（epoch 秒，落库 ts=UTC 该瞬时）、`open_price/high_price/low_price/close_price`、`volume`、`is_blank`（非交易日空 K 线，**落库时跳过**）；另 `security/name/next_req_key`。

**v1.4.93 BUG-002 实测坑**（严格校验逐字段报错，以实测为准）：K 线请求**必须用 `security` 对象**——`symbol` 字符串形态被误报 `unknown field(s): owner`（symbol_normalize 适配器 bug）；`/api/kline` 拒绝 `begin_time/end_time`（未知字段）、`/api/history-kline` 拒绝 `req_num` 与文档声称的 `count` 别名。

### 限频策略（2026-08-01 老板指令）

富途官方限制（协议文档）：**历史 K 线 3103 第 1 页 30 秒内最多 10 次，后续页不限频**；快照 3203 一级 30/二级 20/三级 10 次每 30 秒。实现取更保守值，所有 futu 请求共享全局速率池（`internal/futu/ratelimit.go`）：

- **全部请求**：`QuoteLimit` 20 req/s（50ms/请求）
- **K 线请求**：叠加 `KlineLimit` 5 req/s（200ms/请求）
- **history-kline 第 1 页**：叠加 `HistoryPageLimit` 3s/次（官方 10 次/30s 的均匀化）
- **分页批间**：强制 ≥1s/批（`BatchGap`，含 -every 循环）
- **超限响应**：HTTP 429 → 1s/2s 指数退避重试至多 3 次后报错停止，不硬拉

拉超会被富途限制行情权限，属安全红线；改动限频参数须先确认官方文档当前数值。


## 手动模式（图形验证码 + telnet 控制口，2026-08-01 实测）

ostai 镜像的 8000 WebSocket 通道**只处理短信验证码**（`req_phone_verify_code`），**不处理图形验证码**（`req_pic_verify_code`——登录触发安全验证时的图片码）。需要手动模式：

**直接运行 OpenD 二进制并开启 telnet 控制口**——已记录为 compose：`configs/docker-compose.futu-manual.yml`：

```bash
# 1. 渲染凭证（~/.wbot/config.yaml → ~/.wbot/.env）
umask 077 && tools/config-to-env.sh ~/.wbot/config.yaml > ~/.wbot/.env
# 2. 启动（direct 二进制 + telnet 22222）
docker compose --env-file ~/.wbot/.env -f configs/docker-compose.futu-manual.yml up -d
```

**登录流程**：
1. `docker logs -f futu-opend`——看到 `Need a graphic verification code` 表示需图码
2. `docker cp futu-opend:/root/.com.futunn.FutuOpenD/F3CNN/PicVerifyCode.png ~/pic.png` 取图（每次容器启动验证码会变）
3. 提交：`telnet 127.0.0.1 22222` → 输入 `input_pic_verify_code -code=XXXX` → `Login successful` 即成功
   - VM 无 telnet 时：`bash -c 'exec 3<>/dev/tcp/127.0.0.1/22222; printf "input_pic_verify_code -code=XXXX\n" >&3; cat <&3'`
4. 失败清理（密码错误会扣登录机会）：`docker rm -f futu-opend`

**登录机会**：密码错误有次数限制（`N chances remained`），**停止重试**避免耗尽；先确认 App 密码与 MD5 一致再启动。
## arm64 宿主说明（2026-08-01 实测）

- OpenD 官方仅发布 **x86_64** 二进制；在 Apple Silicon / ARM 宿主（如 OrbStack Linux VM aarch64）上需 **amd64 模拟**：compose 已含 `platform: linux/amd64`（OrbStack/QEMU 自动模拟）
- 验证：`docker run --platform linux/amd64 --rm ostai/futuopend:9.4.5418 echo OK`
- 登录失败（密码错误/图形验证码）时的日志关键字：`Login failed`、`Need a graphic verification code`；验证码图片在容器 `/root/.com.futunn.FutuOpenD/F3CNN/PicVerifyCode.png`（`docker cp` 取出查看）


## 交易安全策略（2026-08-01 老板指令）

- **默认使用模拟盘（Paper Trade）**：所有开发/测试交易走模拟盘账户（trd_env=0，如 acc_id 1907141）
- **实盘只读**：实盘账户（trd_env=1）**禁止一切写操作**（下单/改单/撤单/解锁等），仅允许查询（资金/持仓/订单只读）
- **实盘写操作需老板确认**：任何解除实盘写限制的变更（代码/配置/流程）必须经老板在 GitHub（discussions/21 或 issue）明确确认后才可实施
- **工程护栏**（第一个交易命令实现时内置）：
  - 交易类命令默认 `trd_env=0`（模拟盘）；实盘写操作需显式 `--live` 标志 + 启动日志红色告警
  - 网关 REST legacy 模式天然阻止 mutating 端点（`blocked_mutating_endpoints`）——保持该配置，不因解锁而开放
  - 实盘查询类命令（funds/position/order 只读）无需确认，但输出标注账户环境（real/simulate）
- **例外**：老板在场明确指示且确认账户/金额时，可临时放开（执行后恢复默认）
