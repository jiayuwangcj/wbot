# 导航当前页高亮 (S-UI-active-nav) — 2026-08-02

状态: ✅ 已合并 (PR #149, commit e9baac9)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):五页导航(首页/
策略/回测/数据/Admin)无当前页指示,多页切换时定位迷失。富途/IB
工作台均有当前 tab 高亮。低成本高感知改进。

## 改动
1. **app.js**: `setActiveNav()`——按 location.pathname 匹配 header nav
   链接加 active 类;`/ui/` 与 `/ui/index.html` 双向归一为同页
   (直接访问 index.html 的场景也高亮)。文件末尾统一调用一次
   (所有页面共用 nav,仅 pathname 不同)。
2. **style.css**: `nav a.active` 规则(下划线 + 字重 + underline-offset,
   桌面/移动端一致,主题变量下和谐)。
3. 测试: TestActiveNavJS(5 断言 app.js + css 规则断言)。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh smoke 10/10
- 逐端点验收 8/8:serve 吐出的契约(6)+ node 语义验证(6 条路径
  各恰好一个 active,含 /ui/ 与 /ui/index.html 归一)
- CI: 5/5 全 pass 首轮绿;PR #149 merged

## 备注
- 验收小坑:首版只在 href 侧归一,直接访问 `/ui/index.html` 时 path
  侧未归一 → 无高亮;node 模拟恰好抓住。修复为双向归一
  (`path` 与 `want` 都归一 index.html → /ui)。
- 用 pathname 匹配而非字符串 includes,避免 `/ui/results.html` 误命中
  `/ui/results-filter` 之类前缀。
