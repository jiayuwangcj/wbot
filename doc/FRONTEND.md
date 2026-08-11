# 前端工程规范

## 目标与边界

前端位于仓库根 `web/`，使用 Vite MPA 生成五个入口，静态产物唯一写入 `internal/webui/web/dist/`。Go 通过 `//go:embed web/dist/*` 提供 `/ui/`；页面不改变现有 URL、hash 深链、静态文件 404 或 `stampedFS` 的 304 语义。

产品 UI 只做只读账户/行情/审计展示和结构化 Wheel 配置写入。浏览器永远不调用 `/v1/futu/quote`；行情页面只使用后端允许的代理接口。Admin 配置 GET 只返回 key 元数据，输入值只写入、保存后立即清空，绝不读回或回显。LLM key 只从环境变量读取，不能落库、打印或进入 bundle。

## TypeScript 约定

- `strict` 全开，并额外启用 `noUncheckedIndexedAccess`、`exactOptionalPropertyTypes`、`noImplicitOverride`、`noImplicitReturns` 和 `verbatimModuleSyntax`。
- API 边界使用 `unknown` 解码后做类型守卫；动态 JSON 使用 `Record<string, unknown>` 或 `JsonValue`，不得把 `any` 传播到页面。
- `any` 白名单只有第三方库声明无法表达的边界适配；必须在同一行写明库名、原因和删除条件，并限制在 `src/api/`。当前实现没有使用 `any`。
- 页面展示层不能改变 API 原始值；格式化只在 `src/lib/format.ts` 做，排序使用原始字段。
- 注释最多一行，只解释不能从代码直接读出的契约或安全原因。

## 目录所有权

```text
web/src/api/                 所有 HTTP 请求、响应类型和错误边界
web/src/hooks/               跨页面状态生命周期
web/src/lib/                 无副作用的格式化、主题和导航工具
web/src/components/          跨页面组件；切片 0 后冻结公共签名
web/src/pages/<page>/**      页面私有组件、组合逻辑和测试
web/src/styles/              只引用 public/style.css 的 CSS 变量
web/public/                  URL 不变的 favicon.svg、style.css
```

页面切片只拥有自己的 `pages/<page>/**` 与入口 HTML。共享层签名不可由页面切片直接修改；若新页面需要共享能力，先提交独立的共享层变更并补充行为测试，再由主会话串行合入。页面之间不得互相 import。

组件禁止直接使用 `fetch`、`XMLHttpRequest` 或拼接 API 错误。组件只能调用 `src/api/index.ts` 的类型化函数；API 类型集中在 `src/api/types.ts`，作为前端契约单一来源，并与 [doc/API.md](API.md) 的 17 个 UI surface 对齐：strategies、account、snapshots、orders、runs、bars、datacheck、ingest、admin config read/write、admin cluster、watchlist read/write/delete、wheel configs、wheel signals/actions、backtests list/write/detail/export。

## API 错误约定

`fetchJSON<T>` 把网络不可达规范化为 `cannot reach the server`，非 JSON 成功响应规范化为 `unexpected server response`。错误响应优先读取 `{code, message, action}`，显示文案为 `message · action`；`error` 只作为兼容别名。页面捕获 `ApiError` 后同时保留可操作的 `action`，不得只显示状态码。

## 主题与视觉

`web/public/style.css` 是 CSS token 的唯一来源：背景、表面、文字、边框、accent、成功、警告、错误和阴影都必须通过 `var(--...)` 使用。Ant Design 的 token 由 `src/lib/themeTokens.ts` 从 CSS 变量派生，禁止在组件中新增颜色字面量。主题有浅色、深色和高对比深色三种模式；`useTheme` 默认跟随 `prefers-color-scheme`，手动选择持久化到 `localStorage` 的 `wbot-theme`，并同步 `html[data-theme]` 与 antd algorithm。

页面主视觉优先级是指标卡、主图、状态信息；表格和参数表单应放入 Card、Tabs、Drawer 或折叠区域，避免把所有功能平铺在首屏。表格数字使用 `tabular-nums`，正负值分别使用 `--ok` / `--down` 语义色。移动端保持 44px 触控目标、表格容器内横向滚动和 767px/1024px 断点。

## Hook 生命周期

`useAutoRefresh` 默认 30 秒，只在页面可见时运行；`visibilitychange` 隐藏时清除 timer、返回时恢复并刷新。组件卸载必须清除 interval 和事件监听，不得在页面私有代码自行创建未清理的轮询。

`useAsyncData` 返回 `{data, loading, error, refresh}`。首载和手动刷新使用同一 loader；加载失败保留 `ApiError.message/action`。空态、加载态和错误态必须分别可见，首载错误不能被自动刷新静默吞掉。

图表包装层只创建一次 Lightweight Charts 实例，换数据使用 `setData`，换主题使用 `applyOptions`，不得因主题切换丢失缩放状态。`LineChart` 必须保留键盘左右箭头逐点浏览和读数；`KlineChart` 使用同一套 CSS token。

`WheelForm` 是共享组件，固定包含价格—目标库存曲线和 9 个标量字段。提交校验必须保留 checklist 中的 15 条中文原文：有效数字、最大库存、合约乘数、DTE、最低期权质量、正常/极端日张数、不交易缺口、最少锚点、曲线数字/价格/递增/单调/范围和战略状态。页面只消费 `WheelParams`，不能复制一份校验器。

## 测试分工

- Go 测试验证静态树、标题/lang/viewport/favicon、`/ui/` 页面 200、缺失文件 404、`Cache-Control: no-cache`、`Last-Modified`/304、embed 资产清单和 API/CLI 契约。
- Vitest + Testing Library 验证组件行为：`ApiError`、格式化、主题持久化/系统变化、自动刷新 timer 清理、异步数据状态、Wheel 15 条校验、DataTable 空/加载/排序语义、图表数据更新和管理员值不回显。
- `scripts/accept-*.sh` 是 HTTP/CLI 契约验收，不依赖 UI 实现；React 重构不能删、改或降低其覆盖。

## 构建与体积纪律

Go 编译依赖 `internal/webui/web/dist`，顺序固定为：`cd web && npm ci && npm run build`，再运行 `gofmt`、Go 测试或构建。`scripts/dev-up.sh`、`scripts/verify.sh`、`scripts/release.sh` 和 Makefile 都必须先生成 dist；CI 的 frontend job 生成并上传 dist，`test` 与 `db-integration` 下载同一 artifact 后再编译 Go。dist 不允许手工编辑，旧 `app.js`、vendor chart 和旧 HTML 不得回归。

每页首屏 bundle 目标为 `<800 KB`，按 Vite 的页面入口 JS 记录；切片 0 基线如下（原始字节，括号内为 gzip）：

| 页面入口 | JS | gzip |
| --- | ---: | ---: |
| dashboard | 638.5 KB | 200.5 KB |
| watchlist / results / data / admin | 0.4 KB | 0.3 KB |
| 所有页面共享 AppLayout | 452.9 KB | 148.6 KB |

共享入口另计，页面入口均低于 800 KB；后续页面切片若增加独有 bundle，必须说明 antd/chart 引入原因并优化 shared chunk 或入口依赖。
