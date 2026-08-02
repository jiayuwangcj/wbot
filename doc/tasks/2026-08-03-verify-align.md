# 闭环 #44: verify.sh 与 ci.yml test job 冒烟对齐

- **日期**: 2026-08-03
- **PR**: #208(脚本 + 归档合一)
- **背景**: 「对账」引擎新维度——文档声称的**等价关系**本身要对账: AUTO_ADVANCE 声称「本地 scripts/verify.sh(与 ci.yml test 等价)」,但 diff 两边冒烟清单发现本地缺 CI 的两项: `ingest mock -provider nope`(未注册 provider → exit 2)与 `configyaml` 渲染。race/staticcheck 刻意留 CI(重),冒烟部分不等价。

## 改动

`scripts/verify.sh`: 补两项冒烟(provider 校验 exit-2 + configyaml),加对齐注释(2026-08-03 对账补齐)。

## 验证

- verify.sh 本地全过(19 包测试 + vet + 全部 CLI 冒烟)
- CI 5/5 全绿

## 备注

- **引擎经验**: 「对账」不只对账代码 vs 文档,还要对账**文档声称的等价/同步关系** vs 实际——verify.sh ≡ ci.yml 的声称经 diff 后修齐;race/staticcheck 的 CI-only 设计(重量级)在注释里写明,防后续把「不等价」当 bug 报。
- **候选池**: 仍枯竭。
