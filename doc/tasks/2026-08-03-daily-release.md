# 闭环 #69: 日构建 vdaily-20260803 发布 + 本地部署

- **日期**: 2026-08-03
- **PR**: 本文档(归档);Release: [vdaily-20260803](https://github.com/jiayuwangcj/wbot/releases/tag/vdaily-20260803)
- **背景**: AUTO_ADVANCE 运维沉淀维度对账——RELEASE_DAILY「每日 0 点后阻碍性工作清零 → 发日构建 tag」。最新 tag 为 vdaily-20260802,今日已有闭环 #67/#68 共 4 个 PR 合并(#252-#255),满足「新提交 → 发 tag」条件。

## 动作

1. `GH_TOKEN="$(env -u GITHUB_TOKEN gh auth token)" scripts/release.sh publish --version daily-20260803`
   - 5 平台产物(linux amd64/arm64、darwin amd64/arm64、windows amd64)+ SHA256SUMS
   - Release URL: github.com/jiayuwangcj/wbot/releases/tag/vdaily-20260803
2. `scripts/release.sh deploy --version daily-20260803`
   - 下载 → SHA256SUMS 校验 → 解压到 `~/.wbot/releases/daily-20260803/`;`wbot version` 确认可运行
   - 保 7 清理规则自动生效(daily-20260801/02/03 均在)

## 验证

- 发布 exit 0,release 列表 Latest = vdaily-20260803
- 部署目录二进制可执行(version 输出 daily-20260803)

## 备注

- **引擎经验**: 「运维沉淀」维度——RELEASE_DAILY 是流程文档,与真实 tag/Release 状态的差距需要对账(gh release list vs 当日新提交);日构建不产生代码 diff,闭环留痕走 doc/tasks 归档 + 进度帖。
- **候选池**: 仍枯竭(待老板 7 项)。
