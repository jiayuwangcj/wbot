# master 可选 TLS（HTTPS 登记）

- **id**: `2026-04-17-master-optional-tls`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`

## Goal

- `wbot master` 在提供 `-tls-cert` + `-tls-key` 时用 `tls.Listen` 提供 HTTPS；未设置时仍为 HTTP。
- 验收：`./scripts/verify.sh` 通过；单测覆盖标志校验与 TLS 短跑。

## Constraints

- 不引入新依赖；agent 侧仍用标准 `net/http`（用户可对自签证书自定义 `Client` 后续再接）。

## Links

- Driven-By / trigger: 会话 `continue`（[[AUTO_ADVANCE]]）
- PR / branch: main

## State

- **status**: `done`
- **last step**：`master` 增加 `-tls-cert` / `-tls-key`（成对或皆空）；`TestMasterTLS*`、`httpregister.TestRegisterHTTPS`；`verify.sh` 仍用纯 HTTP smoke。

## Next

- 可选：agent 对自签证书提供可配置 `Transport` / `-insecure`（仅 dev）。
