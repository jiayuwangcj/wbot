# 数据新鲜度打点:三页「更新于 HH:MM:SS」 (S-freshness-stamp) — 2026-08-02

状态: ✅ 已合并 (PR #164, commit a4a31f0)

## 背景
AUTO_ADVANCE 根任务循环,候选池枯竭后走老板长期目标「参照富途/IB/嘉信
打磨 UI」:券商面板(富途牛牛/IBKR TWS/嘉信)账户区都显示**最后更新
时间**,数据是否陈旧一眼可见。wbot 之前没有任何「更新于」提示——数据
停更也无感知;配合 #162 的 Data 页自动轮询,时间戳同时验证轮询还活着。

## 改动
1. **助手**(app.js,紧邻 fmtTime):
   - `fmtClock(d)` → `HH:MM:SS` 秒级时钟(30s 轮询粒度下分钟不够,秒
     级才能证明每 tick 都在刷新)。
   - `stampUpdated(id)` → `el.textContent = "更新于 " + fmtClock(...)`;
     元素缺失静默跳过(app.js 四页共享,页面无该 span 时不报错)。
2. **三页 HTML**:刷新按钮旁各加 `<span id="{page}-updated"
   class="muted">`(dash/admin/data-updated)。
3. **打点接线**(成功路径语义):
   - `loadDashboard` 末尾:双环境查询完成后(含双失败横幅场景)。
   - `loadAll`(Admin)末尾:cluster/config 两个 loadJSON 之后。
   - `loadDataCoverage` 末尾:仅成功路径——失败不更新时间戳,用户
     能看出「最后一次成功」时刻。
4. 契约测试:三页 HTML 槽断言 + `TestFreshnessStampJS`(助手/秒级时钟/
   三处接线)+ TestAdminAutoRefreshJS/TestDataPageContract 扩展。

## 验证
- `go test ./... -count=1` 全绿(19 包,含 PG 集成)
- dev-up smoke 10/10(二进制变更自动重启生效)
- 逐端点验收 9/9:三页槽 ×3、app.js 助手/接线 ×5、loadDataCoverage
  函数体末尾打点 ×1
- CI: 5/5 全 pass 首轮绿;PR #164 merged

## 备注
- **为什么秒级**:轮询 30s 一次,分钟级时间戳大部分刷新不变化,
  无法证明轮询活跃;秒级每次刷新都变(除非恰好同一秒)。
- **为什么成功路径才打点**:时间戳语义是「数据最后更新时间」,
  失败刷新不算;失败仍由既有 error 元素提示,不冲突。
- **验收脚本坑**:grep 把含 `[`/`[^` 的多行字符串当正则 → `has`
  助手必须 `grep -qF`(固定字符串)。
