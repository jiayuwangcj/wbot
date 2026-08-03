# 2026-08-03: 文档「代浏览器/可视化切片」误述整链清零 (#307)

## 来源

Round 31 triage:候选池与排期项全部核实已实现或待拍板后,跑防御性端点对账(API.md 声明端点 vs serve mux 注册 vs 前端 fetch 引用)。结果:24 个端点双向全部命中,但抓出三处过时表述:

1. **API.md:595** `/v1/futu/options` 描述为「期权链可视化切片…serve 代浏览器调用」——策略页 options 链视图已按老板决策删除(960710c,2026-08-02「不需看盘工具」)
2. **API.md:454** `/v1/futu/quote`「serve 代浏览器先订阅后取 Basic 快照」——报价卡已随 Dashboard 改造删除(2a15758「Data 页改 Dashboard」,bars/quote 区块已删)
3. **FUTU.md:182/185** serve 代理总述「代浏览器访问」+ quote 行「数据页 bars 表单提交同时刷新报价卡」——同上

前端当前仅调用 `/v1/futu/account` / `/v1/futu/orders`(app.js 零 quote/options 引用,实测确认)。

## 修法

| 文件 | 改动 |
|---|---|
| `doc/API.md` | 总述区分「浏览器 Dashboard 用 account/orders」vs「CLI/脚本消费 quote/options」;quote/options 两段注明消费方与移除决策 |
| `doc/FUTU.md` | serve 代理总述中性化;quote 行删除过时报价卡表述 |
| `doc/ROADMAP.md` | v4 行沉淀老板决策「**不做看盘工具**」(2026-08-02)——防止未来候选步重复提议恢复期权链视图 |

API.md:487 account 段的「代浏览器」表述**保留**——该端点确实被 Dashboard 页调用(逐段核验,非整批替换)。

## 验证

- 复扫「代浏览器/可视化切片/报价卡/刷新报价」全库清零(account 段为准确命中保留)
- verify.sh 连跑两遍全绿(纯文档,无代码)
- CI 五检查全绿;docs-only 不 republish

## 引擎经验

对账发现「文档说前端有 X、前端没有」时,先 `git log -S` 定位删除提交,确认是否**老板决策删除**(960710c 删除 options 链、2a15758 删除 bars/quote 均是)——决策性删除要改文档表述并把决策沉淀到 ROADMAP,而非恢复功能。
