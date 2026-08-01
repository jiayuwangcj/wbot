# 敏感配置与密钥安全（不入库）

**约定（2026-07-31 用户确认）**：部署资源、微信 token、券商凭证等**密钥/资产配置统一放 `~/.wbot/`**（home 级目录，见 `~/.wbot/README.md`）；**仓库内禁止提交任何密钥/资产配置值**。

## 红线

- 禁止入库/入 commit message/入 PR 描述：token、密钥、私钥、口令、含口令的 DSN、云凭证、资产配置**值**
- 仓库内允许：环境变量**名**（如 `WBOT_PG_DSN`）、测试假值占位（如 `postgres://postgres:postgres@localhost...` 仅限测试/CI 服务容器）
- `.gitignore` 覆盖常见敏感模式（`*.env`、`*.key`、`*.pem`、`secrets*` 等）

## 评审检查（reviewer 必查）

- diff 扫描：密钥/资产配置类泄漏（值形态：`sk-`、`token`、`key`、`password`、`secret`、私钥块、含口令 DSN）；发现 → **P0 阻断合入**
- 新引入敏感文件/目录须在 `.gitignore` 或 `~/.wbot` 约定内；测试 fixture 用假值
- 环境变量名引用（读取侧）合规，不构成泄漏

## ~/.wbot/config.yaml（部署级配置；2026-08-01 起替代 env.sh）

部署资源、券商凭证等**部署级配置**统一写 `~/.wbot/config.yaml`（YAML，600 权限，仅本人可读）：

```yaml
futu:
  login_account: "${FUTU_LOGIN_ACCOUNT}"      # ${VAR}：值来自环境变量；未定义 → 报错（含变量名）
  login_region: "${FUTU_LOGIN_REGION:-sh}"    # ${VAR:-default}：未设置/空时用默认值
```

- 解析实现：`internal/configyaml`；`wbot configyaml` / `tools/config-to-env.sh` 输出纯 `KEY=VALUE` 行（供 `docker compose --env-file` 或 shell source）；嵌套路径扁平为 UPPER_SNAKE（`futu.login_account` → `FUTU_LOGIN_ACCOUNT`）
- 示例模板：`tools/config.yaml.example`（只含占位，可安全入库）；真实文件权限须 0600，渲染工具校验不通过即报错
- 与 ⑥-B 管理配置并存、分工不同：`~/.wbot/wbot.conf`（JSON，admin API 读写，见 [[API]]）是运行时配置；本文件是部署级凭证注入层（链路见 [[FUTU]]）

## 各角色

- 编码/运维：敏感值经 `~/.wbot/config.yaml`（`${VAR}` 引用宿主环境变量）注入，不写死代码
- 评审组：按上文检查；发现泄漏 → P0 并提示清理与轮换
- 产品组/主 agent：对外文档不出现真实值

关联：[[ORGS]] [[GITHUB_SETUP]] [[README]] [[FUTU]]
