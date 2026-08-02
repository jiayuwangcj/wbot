# 闭环 #77: serve -h 端点形状精确化

- **日期**: 2026-08-03
- **PR**: #271(功能)+ 本文档(归档)
- **背景**: #76 补齐 serve -h 端点清单后,继续逐项比对发现**清单已全但形状不精确**:「GET/PUT /v1/watchlist」「GET/PUT /v1/admin/config」与实现不符——PUT/DELETE watchlist 实际在 `/v1/watchlist/{symbol}`(watchlist.go:106 switch),PUT admin config 实际在 `/v1/admin/config/{key}`(admin_config.go:42);无路径 PUT 会 405,help 简写误导。API.md 本就有精确章节(`## PUT /v1/watchlist/{symbol}`、`## PUT /v1/admin/config/{key}`)。

## 改动

- cmd/wbot/main.go serve -h 统一精确形态:`GET /v1/watchlist, PUT/DELETE /v1/watchlist/{symbol}`;`GET /v1/admin/config, PUT /v1/admin/config/{key}`

## 验证

- `wbot serve -h` 实测输出精确;verify.sh 全绿;dev-up 自动重启后 ALL 19 CHECKS PASSED;PR #271 CI 5/5

## 备注

- **引擎经验**: help 端点清单不仅要「端点存在」(mux 注册比对),还要「**形状精确**」——方法与路径参数位置要和 handler 的 method switch 逐项核对;文档(API.md)与 help 双源对照,先精确的那一方作基准。
- **候选池**: 仍枯竭(待老板 7 项)。
