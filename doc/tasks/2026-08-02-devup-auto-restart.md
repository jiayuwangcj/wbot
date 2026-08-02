# dev-up 二进制变化自动重启 serve (S-devup-auto-restart) — 2026-08-02

状态: ✅ 已合并 (PR #160, commit b53532f)

## 背景
AUTO_ADVANCE 验收规则「运维沉淀」:服务端改动后 `scripts/dev-up.sh`
默认复用旧 serve 进程(仅 `--force` 重启),逐端点验收会误判旧行为。
本会话实际踩过:S-backtests-search 首轮验收 7 项误失败(q 参数无效,
serve 还是旧二进制)。修复为自动检测。

## 改动
`scripts/dev-up.sh`:
1. build 前记旧二进制 `md5sum`,build 后取新值。
2. `already_up==1 && force==0` 分支:新旧 md5 不同 → 打印
   "rebuilt binary differs from running serve; restarting" 并置
   `force=1` 走既有重启逻辑;相同 → 原「already up」提示。
3. 原 if/elif 链拆为两个独立 if(置 force 后需重新评估分支)。

## 验证
- 连续两次 dev-up:一跑(有差异时)自动重启,二跑幂等
  ("serve already up")——Go 构建确定性(md5 可比较)成立。
- 真实源码改动(追加注释行)→ 触发 restart → 还原后恢复幂等。
- smoke 10/10 每轮。

## 备注
- **为什么不看 mtime**:dev-up 每次跑都 `go build`,输出文件 mtime
  恒更新 → 用 mtime 判断会每次都重启。md5 按内容判断,无改动
  时幂等。
- **为什么不看版本号**:dev 构建 version 恒为 `0.0.0-dev`
  (无 -ldflags 注入),无法区分构建。
- 验收脚本里的 serve 重启惯例更新:服务端改动后无需再手动
  `--force`,dev-up 自动处理。
