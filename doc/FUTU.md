# Futu OpenD 容器部署（⑪-a）

富途接入的**连接层**：OpenD（Futu 行情网关）以容器方式跑在本地，⑪-b 的 `wbot futu` 经 11111 端口 protobuf API 连接。对应 `doc/tasks/2026-07-31-futu-integration.md` ⑪-a（⑪-b/c 待账号与 go.mod 决策），行情落库见 [[DATA_PIPELINE]]。

## 前置条件

- Docker + Compose v2（`docker compose version` 可跑）
- 富途账号 + 密码 + 首登短信验证码（老板侧，见 [[ORGS]]）
- 敏感值只放 `~/.wbot/env.sh`（[[PRIVACY]] 红线：仓库与镜像配置**零凭证值**）

## 1. 凭证注入（~/.wbot/env.sh）

compose 用 `${VAR:?}` 占位强制注入，真实值来自宿主环境变量：

```bash
mkdir -p ~/.wbot
cat > ~/.wbot/env.sh <<'EOF'
# Futu OpenD 登录凭证（值不入库，见 doc/PRIVACY.md）
export FUTU_LOGIN_ACCOUNT='<富途账号>'
export FUTU_LOGIN_PWD_MD5='<密码明文 MD5>'   # 生成：echo -n '<明文密码>' | md5sum | awk '{print $1}'
export FUTU_LOGIN_REGION='sh'                # 可选：sh=上海(默认) | hk | us
EOF
chmod 600 ~/.wbot/env.sh    # 仅本人可读
```

要点：

- MD5 必须对密码明文计算且**不含换行**：`echo -n '<明文>' | md5sum`（`echo` 漏写 `-n` 会算出错误散列）；MD5 本身等同凭证，同样只进 env.sh
- 任何 compose 操作前先 `source ~/.wbot/env.sh`（`FUTU_LOGIN_REGION` 不设时 compose 默认 `sh`）

## 2. 拉取与启动

```bash
source ~/.wbot/env.sh
docker compose -f configs/docker-compose.futu.yml pull     # 首次拉取 ostai/futuopend:9.4.5418
docker compose -f configs/docker-compose.futu.yml up -d    # 后台启动
docker compose -f configs/docker-compose.futu.yml logs -f futu-opend
```

| 端口 | 用途 |
| --- | --- |
| `11111` | OpenD protobuf API（`wbot futu` 客户端连接口） |
| `8000` | 首登短信验证码 WebSocket（见下节） |
| `22222` | telnet 控制台（compose 中默认注释，调试可放开） |

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
# 状态一：未加载 env → 期望显式报错（`${VAR:?}` 生效即证明注入机制正确）
docker compose -f configs/docker-compose.futu.yml config
# => Error: required variable FUTU_LOGIN_ACCOUNT is missing a value: ...

# 状态二：已 source env.sh（值为非空占位即可，无需真实凭证）→ 正常渲染
source ~/.wbot/env.sh
docker compose -f configs/docker-compose.futu.yml config
```

备选注入方式：`docker compose --env-file <file>`（文件须为纯 `VAR=value` 行、无 `export` 前缀；`~/.wbot/env.sh` 是 `export` 语法故不能直接复用，若走此路径另备一份同权限 600 的 env 文件）。

## 5. 常见错误

| 现象 | 原因与处理 |
| --- | --- |
| `required variable FUTU_LOGIN_ACCOUNT is missing a value` | env 未注入：`source ~/.wbot/env.sh` 后再执行 |
| 登录失败 / 账号或密码错误 | MD5 算错（换行、大小写、空格）：按第 1 节重算 |
| 验证码错误或过期 | 重连 8000 重新提交新短信验证码 |
| 行情报未开通市场（美股/港股等） | 账号未开通对应市场权限，**属预期错误**；向老板确认开通（`doc/tasks/2026-07-31-futu-integration.md` ⑪-c 文档注明） |
| 容器反复重启 | `docker compose logs futu-opend` 看日志；确认宿主 `~/.com.futunn.FutuOpenD` 目录可写 |
| 11111/8000 端口被占 | `ss -tlnp` 后过滤 11111/8000，定位占用进程 |

## 6. 连通性验证（⑪-b 前置）

```bash
nc -zv 127.0.0.1 11111    # API 端口可达即网关就绪
```

安全提醒：本文件与 compose 只含变量名与 `${VAR:?}` 占位，零凭证值；真实值仅存 `~/.wbot/env.sh`（600 权限），永不入 commit/PR。关联 [[PRIVACY]]。
