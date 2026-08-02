# 闭环 #46: Admin 配置写面(只写不读,PRIVACY-safe)

- **日期**: 2026-08-03
- **PR**: #212(功能)+ 本文档(归档)
- **背景**: 「UI 交互打磨」引擎对账发现——Admin 页配置区只读(键元数据表),但 PUT /v1/admin/config/{key} 写面早已存在(#30 之前落地)。券商面板惯例「设置页可写」缺口:管理员要改凭证只能手动编辑 ~/.wbot/wbot.conf。补最小写面。

## 改动

- **admin.html**: 配置区新增 `config-set-form`(键下拉 select + 值输入 + 设置按钮 + 「已保存。」提示 span);提示语改「仅元数据:配置值只写不读——设置时输入,永不展示/回显(凭证、监听地址)。」
- **app.js**: `initConfigForm()`——`isSecret = (k) => k.startsWith("credentials.")` → 凭证键自动切 `password` 输入(autocomplete off);提交走 `fetchJSON` PUT(值 JSON body),「保存中…」忙态(btn.disabled + textContent),成功后**清空输入**(值不留 DOM,隐私)+ `loadConfig()` 回填列表;`renderConfig` 通过 `form.hidden` 守卫首次渲染时初始化表单(30s 自动轮询不打断输入);`loadConfig` 抽共用供 Promise.all
- **webui_test.go**: `TestConfigWriteSurfaceJS` 新契约断言(只写不读语义/清空输入/忙态/URL 编码键);`TestAdminPageReadOnly` 放宽为「除 config-set-form 外无表单」——设计随 PUT API 演进

## PRIVACY 红线

- 值只写不读: GET 仅元数据(Entry 永不含值)、PUT 响应无值、表单从不展示既有值、成功即清空输入
- 凭证键用 password 输入 + autocomplete off,防 DOM/密码管理器泄露

## 验证

- 19 包测试全绿 + gofmt clean
- dev-up --force smoke 16/16
- 配置写面端到端(真实 serve): PUT `credentials.schwab.api_key` → `{"key":...,"set":true}`;GET 列表 set=true 无值;wbot.conf 0600 落盘带值;空值 400;白名单外 `nope.nope` 404;测试 wbot.conf 清理后 set 计数 0
- embed 内容: admin.html 4 匹配 / app.js 6 匹配(`initConfigForm|loadConfig|保存中`)
- CI 5/5 全绿

## 备注

- **引擎经验**: UI 写面必须保留「只写不读」语义——写面落地时同步检查值渲染路径(此处靠清空输入 + API 无值);Admin 只读测试约束(`TestAdminPageReadOnly` 无 form)随设计演进更新,而非固守旧断言。
- **候选池**: 仍枯竭。后续同引擎候选(备忘,非本轮):Admin 配置区「已设置」badge 数量角标、键搜索过滤。
