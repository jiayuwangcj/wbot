# 2026-08-03: config.yaml 网关地址接入 (#305)

## 背景

对账扫描命中 API.md:462「config.yaml 接入后续切片」——serve 读 `$FUTU_GATEWAY_URL` / `$FUTU_PROTO_ADDR`,但 config.yaml 只能管理 4 个登录凭证变量,网关地址只能 env 或 CLI `-addr`。

## 核实(诚实欠账确认)

实测 `configyaml` 渲染:只有 `FUTU_LOGIN_ACCOUNT / FUTU_LOGIN_PWD_MD5 / FUTU_LOGIN_REGION / FUTU_INIT_ON_START` 4 行。确属诚实欠账,非过时表述——修之前先跑 configyaml 验证实际渲染(对账引擎纪律)。

## 实施

configyaml 是通用 flatten:任何 `futu.xxx` 键自动渲染成 `FUTU_XXX`,零 Go 代码改动。改动面:

| 文件 | 改动 |
|---|---|
| `tools/config.yaml.example` | 加 `gateway_url: "${FUTU_GATEWAY_URL:-http://127.0.0.1:22222}"` + `proto_addr: "${FUTU_PROTO_ADDR:-127.0.0.1:11111}"` |
| `internal/configyaml/configyaml_test.go` | 新增 `TestLoadGatewayAddrs`:默认值渲染断言 + `t.Setenv` 后 OrbStack 桥接形态(192.168.215.2)覆盖断言 |
| `doc/API.md:462` | 「后续切片」→ 已落地说明(compose `--env-file` 注入;CLI 直跑仍 `-addr`) |
| `doc/FUTU.md` §1/§7 | config 示例块加两行注释 + §7:178「两键均可写进 config.yaml…OrbStack 等非默认网关场景一处管理」 |

## 验证

- `go test ./internal/configyaml/` 全绿
- 端到端渲染:`tools/config-to-env.sh` 对 example 文件实测输出含 `FUTU_GATEWAY_URL=http://127.0.0.1:22222` / `FUTU_PROTO_ADDR=127.0.0.1:11111`
- verify.sh 连跑两遍全绿
- 全库复扫「后续切片」表述清零
- CI:check-skip / ci-summary / db-integration / governance / test 五检查全绿

## 遗留

- 配置写面仍保持「只写不读」语义:配置值永不返回 API(隐私红线)
- 示例文件权限 0600 要求不变(configyaml 强制);测试渲染临时文件跑完即删
