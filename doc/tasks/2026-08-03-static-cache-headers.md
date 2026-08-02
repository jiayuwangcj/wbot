# 闭环 #83: 静态资源缓存语义(构建时间戳 Last-Modified + no-cache)

- **日期**: 2026-08-03
- **PR**: #283(功能)+ 本文档(归档)
- **背景**: 运维对账——`/ui/*` 由 `http.FileServer(http.FS(webRoot))` 直出,go:embed 文件 **modtime 恒为零** → Go 的 ServeContent 不发 Last-Modified、不响应 If-Modified-Since → 浏览器每次页面加载全量 200 重下 style.css/app.js(实测响应头除 Date 外无任何缓存相关字段),永不 304。

## 改动

- internal/webui/webui.go:`stampedFS`(fs.FS 包装,叠加固定 ModTime = **二进制 mtime 即构建时刻**)恢复 http.FileServer 的条件请求机制;外层中间件统一 `Cache-Control: no-cache`。组合语义:不变二进制 → 每载 304(空 body 极廉价);重建(新 mtime)→ 部署后首个请求即新资源,**改前端后拿旧版 CSS/JS 构造上不可能**。
- internal/webui/webui_test.go:`TestCacheRevalidation`(Last-Modified 存在 / 条件请求 304 / 304 body 空)
- scripts/dev-up.sh:冒烟 19 → 22 项(no-cache 头 / Last-Modified 存在 / 条件请求 304 三连)
- README.md + doc/RELEASE_DAILY.md + doc/ACCEPTANCE.md:冒烟计数 19 → 22(4 处数字同步)

## 验证

- verify.sh 全绿;dev-up 22 项连跑两遍全过(serve 实测三连头齐全:no-cache + Last-Modified + 304);PR #283 CI 五 job 全绿

## 备注

- **引擎经验(运维对账)**: go:embed 资源的零 modtime 是隐蔽坑——FileServer 照常 200 返内容,但缓存链路的验证器整环缺失;「无缓存头」类问题要实测响应头,不能只看代码路径。缓存语义设计上 **no-cache + 验证器** 是 dev/部署工具的安全组合,naive max-age 反而制造脏缓存窗口。
- **候选池**: 仍枯竭(待老板 7 项)。
