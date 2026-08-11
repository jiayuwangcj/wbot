# React 重构功能等价对照清单(切片 P1–P4 验收核对单)

- 来源:Sol 产品评估(2026-08-11)建议实体化;实读 app.js 6cde23d/行号 + 5 个 HTML 页面 + style.css
- 用法:每切片交付后逐项核对等价,核对项打 [x];不留口头「应该可以」
- 说明:锚点「app.js:N-M」为函数体行号,「html 行」为对应页面静态结构;空态文案原样摘录

## Dashboard(目标 ~12 项,实计 12 项)

1. 环境切换 Paper 模拟盘 / 实盘 Live(默认 sim 安全红线;badge 文案 PAPER · 模拟盘 / REAL · 实盘;切换联动聚合卡/持仓/订单/资产曲线) | app.js:204-215 renderBadge、469-484 initDashboardPage | 无
2. 账户资产指标卡 5 项(总资产/现金/证券市值/购买力/可用资金,金额千分位 2 位小数) | app.js:217-234 renderSummary、153-155 fmtAccountMoney | 无
3. 资产曲线 sparkline(DB 快照 /v1/account/snapshots?limit=120;≥2 点才画,<2 点显示引导;范围标签「HH:MM:SS → HH:MM:SS · N 点」) | app.js:238-259 renderSummaryCurve、196-202 loadSnapSeries | 「暂无历史快照;可运行 `wbot ingest account` 开始记录(支持 -every 定时)。」(index.html:51)
4. 资产曲线 hover/touch 读数(tip 显示「时刻 · 总资产」,touch 即触即显) | app.js:264-284 attachCurveHover | 无
5. 子账户明细表(sim/real 各一行:环境/账户 ID/总资产/现金/市值/可用资金/购买力/持仓数/状态;失败行「不可用」+title 错误、未载入「加载中」) | app.js:286-324 renderAccounts | 「暂无账户数据。」(index.html:58)
6. 持仓表(代码/数量/成本价/现价/市值/盈亏;盈亏/收益率语义色 num-up 绿/num-down 红;默认按市值降序) | app.js:326-356 renderPositions、360-367 numCell、2133-2140 POSITIONS_SORT_KEYS | 「当前环境无持仓。」(index.html:72)
7. 当前订单表(时间/代码/方向/状态/数量/价格/已成交;方向中文化 buy→买入/sell→卖出,买入侧语义色 side-buy/side-sell;默认按时间降序新单在上) | app.js:382-398 renderOrders、371-380 SIDE_ZH/STATUS_ZH、2143-2151 ORDERS_SORT_KEYS | 「暂无挂单。」(index.html:89);区块说明「挂单列表（read-only, /v1/futu/orders;模拟盘默认）。」(index.html:87)
8. 最近入库表(ID/来源/状态/开始/结束;状态中文化 succeeded→成功/failed→失败/running→运行中,未结束显示「运行中」) | app.js:429-431 renderRuns、loadJSON /v1/runs?limit=10(497) | 「尚无入库记录。」(index.html:105)
9. 手动刷新按钮(点击禁用+「刷新中…」忙态,完成/失败恢复) | app.js:442-454 wrapRefreshClick | 无
10. 30s 自动轮询(仅页面可见时刷新;visibilitychange 隐藏停止、回来恢复) | app.js:435-462 startAutoRefresh、499-502 | 无
11. 「更新于 HH:MM:SS」新鲜度打点(每次成功刷新后) | app.js:169-176 stampUpdated | 无
12. 双环境均失败顶部横幅(含错误原因,提示检查网关容器) | app.js:421-425 loadDashboard | 「Futu 网关不可用:模拟盘与实盘均查询失败(<err>)。请检查网关容器状态后刷新。」(app.js:423 动态拼装)

## Watchlist(目标 ~18 项,实计 21 项)

1. 策略说明卡片(动态 Wheel · 仅提醒;状态 NORMAL / CAUTION / PAUSE_BUY / EXIT;安全边界:报价不完整或过期时保持 HOLD) | watchlist.html:27-36 | 无
2. 点击策略卡片滚动到编辑区 | app.js:1665-1673 selectWheelCard | 无
3. 添加/编辑表单:代码输入 + 隐藏 strategy=wheel;空代码报「symbol is required」 | app.js:1811-1838 提交处理 | 无
4. 价格—目标库存曲线锚点编辑器(「添加锚点」追加行,新行价格=最后锚点 price+1;「移除」按钮,仅剩 1 行时禁用) | app.js:1266-1299 renderWheelCurve、1800-1810 curve-add | 无;帮助文案「价格必须严格递增，目标库存必须单调不增且位于 0 与最大库存之间。」(watchlist.html:50)
5. Wheel 参数字段 9 项:最大库存/合约乘数/最小 DTE/最大 DTE/最低期权质量/正常日最多张数/极端日最多张数/不交易缺口/战略状态(下拉 NORMAL/CAUTION/PAUSE_BUY/EXIT) | app.js:1310-1329 renderWheelFields;HTML 约束 watchlist.html:54-69 | 无
6. 曲线与参数校验规则(提交时逐条校验,错误原文,原样摘录) | app.js:1340-1397 collectWheelParams | 校验原文:「<字段> 必须是有效数字」(1335)、「最大库存必须大于 0」(1350)、「合约乘数必须是正整数」(1351)、「DTE 必须是 5 到 10 之间的有效范围」(1352)、「最低期权质量必须在 0 到 1 之间」(1355)、「正常日最多张数固定为 1」(1356)、「极端日最多张数必须在 1 到 2 之间」(1357)、「不交易缺口必须不小于 0」(1360)、「至少需要两个价格锚点」(1362)、「曲线第 N 行必须填写有效数字」(1370)、「曲线价格必须大于 0」(1372)、「曲线价格必须严格递增」(1373)、「曲线目标库存必须单调不增」(1374)、「曲线目标库存必须位于 0 与最大库存之间」(1375)、「战略状态无效」(1381)
7. 保存成功提示「已保存 <symbol>(wheel)。」+ 表单重置(清空/恢复默认/聚焦代码输入) | app.js:1829-1837、1782-1788 resetForm | 无
8. 编辑回填:点「编辑」把该标 params 填回表单并滚动到编辑器 | app.js:1772-1780 beginEdit | 无
9. 删除:confirm 确认「从观察列表移除 <symbol>?」后 DELETE,失败显示原因 | app.js:1790-1798 deleteItem | 无
10. 一键回测:POST /v1/backtests(带该标参数),成功后跳 results.html#bt-<id> 自动打开详情 | app.js:1677-1689 runBacktest | 无
11. 观察列表表(代码/策略/配置版本 vN 或 —/能力状态 · 原因/参数 JSON 原文/更新时间/编辑·回测·删除);能力状态缺失兜底 UNKNOWN,原因缺失兜底「未登记原因」 | app.js:1399-1455 renderWatchlist | 「观察列表暂无标的。」(watchlist.html:80)
12. 观察列表计数「N 个标的」(空时不显示) | app.js:1406 | 无
13. 观察列表表头排序(代码/策略/更新时间,默认按更新时间降序) | app.js:1766-1770、2166-2170 WATCHLIST_SORT_KEYS | 无
14. 信号筛选表单(代码留空=全部/动作 ALERT|HOLD/能力状态 READY|DATA_BLOCKED;提交刷新) | app.js:1710-1724 loadWheelSignals | 无
15. 信号审计表(时间/代码/动作/能力状态·阻塞依赖/配置 vN 链接/实际·有效·目标库存/原因/详情·人工记录;库存缺失显示 —) | app.js:1603-1648 renderWheelSignals、1457-1461 wheelInventorySummary | 「尚无 Wheel 信号记录；实时供应商未解锁时这是正常状态。」(watchlist.html:112)
16. 信号行内详情展开/收起(现价/实际/期权Δ/有效/目标/缺口、阻塞依赖、拒绝原因、候选列表[方向·张数·质量·接受/拒绝·strike·到期·Δ·bid/ask·IV·原因]、原因) | app.js:1486-1508 wheelSignalDetail、1512-1531 toggleDetailRow/toggleWheelSignalDetail | 无
17. 人工记录查看:「<symbol> / signal #<id>：尚无人工处置记录。」或「<时间> <action> by <actor> · <note>」列表 | app.js:1698-1708 loadSignalActions | 无
18. 信号 → 配置版本联动:点信号行 vN,配置区过滤到该标的并滚动到该区 | app.js:1728-1732 jumpToConfigVersion | 无
19. 配置版本审计表 + 代码筛选(代码/版本/保存时间/配置摘要「wheel · 曲线 N 锚点 · 最大库存 X」/战略状态/详情) | app.js:1744-1763 loadWheelConfigs、1553-1583 renderWheelConfigs、1535-1539 wheelConfigSummary | 「暂无配置版本记录。」(watchlist.html:129)
20. 配置版本行内详情:config/state 完整 JSON 原文(版本不可变,审计证据) | app.js:1542-1551 wheelConfigDetail | 无
21. 深链:#signal-<id> 自动展开对应信号详情;#config-<symbol>-v<版本> 自动展开该版本原文 | app.js:1596-1601 applySignalDetailHash、1587-1592 applyConfigDetailHash | 无
(附:信号/配置两表排序,默认 created_at 降序 — app.js:1735-1739、1754-1758、2174-2188;已计入全站共享项 5,不单列)

## Results(目标 ~17 项,实计 18 项)

1. 启动回测表单:代码输入 + 「使用观察列表全部标的」勾选(勾选后禁用代码输入与参数区,取消恢复) | app.js:1873-1878 syncRunMode | 无
2. 表单 Wheel 参数区(run- 前缀,字段/校验与 Watchlist 完全一致,复用 collectWheelParams) | app.js:1926-1938、1856-1860 | 无
3. 运行回测两模式反馈:单标的 POST /v1/backtests 完成后打开详情「回测 #<id> 完成,已打开详情。」;from_watchlist 返回 {runs} 刷新列表「完成:<N> 条回测已保存,见下方列表。」+ 滚动到列表 | app.js:1880-1924 | 「symbol is required (或勾选使用观察列表全部标的)」(app.js:1890)
4. 回测列表表(对比勾选列/ID/策略/代码/权益/收益/最大回撤/K 线/创建时间/详情;收益与回撤按 % 显示,缺失显示 —) | app.js:2014-2073 renderResultsList | 「暂无回测结果。可在上方运行,或使用 wbot backtest -save 导入。」(results.html:72)
5. 表头排序:点击表头 → 服务端 sort/order 重排全库(本地 sortItems 兜底);无排序参数保持 API 最新优先 | app.js:2640-2653 loadSorted、2120-2129 RESULTS_SORT_KEYS | 无
6. 搜索过滤:输入 250ms 防抖后服务端全库搜索(q ILIKE);本地即时过滤先反馈;清空恢复最近列表 | app.js:2586-2607 | 「无匹配「<q>」的回测结果。」(app.js:2593 动态)
7. 加载更多:列表满 50 条(50 的倍数)才显示按钮;offset 翻页追加;「加载中…」忙态 | app.js:2611-2639 | 无
8. 对比选择:每行 checkbox,恰好两条才启用「对比所选回测」;超过两条拒绝勾选并提示 | app.js:1993-2012 toggleCompareSelection | 「请选择恰好两条回测进行对比。」(app.js:2000;results.html:145 静态)
9. 对比视图:指标表(期末权益/总收益率/最大回撤/K 线数/参数 JSON,列头 = 每条回测)+ 权益曲线叠加 canvas + 图例(色块+标签);两条以上同色区分 | app.js:2503-2530 renderCompare、2486-2501 renderCompareLegend、2532-2551 openCompare | 「无权益曲线可叠加(旧数据)。」(results.html:149)
10. 详情指标卡(期末权益/总收益率/最大回撤/K 线数) | app.js:2271-2277 renderDetail | 无
11. 权益曲线 canvas(手绘;mousemove 悬停读数「ts · 金额」+ 竖线;mouseleave 隐藏;键盘 ←/→ 逐点移动,canvas 可聚焦) | app.js:2556-2566 renderCurve、2353-2441 drawCurvePlot、2663-2679 | 「该回测无权益曲线(旧数据)。」(results.html:99)
12. 导出:CSV / JSON 直链(/v1/backtests/<id>/export?format=csv|json,服务端 attachment 下载,页面不跳转) | app.js:2305-2312 wireExport | 无
13. 重新运行:详情「重新运行」把代码/参数回填顶部表单并滚动聚焦;非 Wheel 策略报「该回测不是 Wheel 策略，无法回填。」 | app.js:1942-1959 rerunHandler | 无
14. 交易记录表(时间/方向[中文化+语义色]/代码/数量/价格/剩余现金);默认仅渲染最近 100 笔 + 提示「共 N 笔交易,仅显示最近 100 笔。」+「显示全部」展开 | app.js:2204-2236 renderTradesTable | 「未记录交易。」(results.html:110)
15. Wheel 信号轨迹表(时间/动作/能力状态·阻塞/原子快照 key·observed_at/方向/库存[实际·有效·期权Δ]/候选/数量/原因;DATA_BLOCKED 与 HOLD 通过能力状态列区分) | app.js:2240-2269 renderBacktestSignals | 「该回测未保存逐 bar 信号轨迹（旧数据）。」(results.html:122)
16. 参数 JSON 原文展示(格式化缩进) | app.js:2284 | 无
17. 深链 #bt-<id> 自动打开该回测详情(来自 watchlist 一键回测跳转) | app.js:2680-2682 | 无
18. 详情/对比返回列表 + 列表行选中高亮(selected 类,返回列表一眼定位) | app.js:2654-2662、2315-2321 selectResultsRow | 无

## Data(目标 ~13 项,实计 14 项)

1. K 线查询表单:代码/周期(1m/5m/15m/30m/60m/1d/1w/1mo)/复权(fwd 前复权默认/none 不复权/back 后复权)/起止日期(date 输入,留空=最近 100 根) | app.js:1169-1182 initDataPage、1159-1167 barsRangeFromInputs | 无
2. 区间语义:未传日期 → limit=100「最近 100 根」;传任一起止 → 闭区间 RFC3339(当天 00:00:00Z–23:59:59Z)、limit=1000 范围内最近 1000 根;标签「最近 100 根」/「指定区间」 | app.js:1143-1145 loadBars、1180 | 无
3. 数据齐全 datacheck:状态 tag 三态(未配置 idle / 完整 ok / 需关注 warn)+ 5 指标卡(标的/检查项/完整项/缺失/过期)+「检查于 YYYY-MM-DD HH:MM」 | app.js:830-874 renderDatacheck | 「未配置」时「自选列表为空；添加标的后将自动检查行情矩阵。」、「完整」时「当前数据完整。」(app.js:857/862;data.html:67)
4. datacheck 明细表(代码/类型[期权|K 线]/周期/复权/状态[缺失 state-down|过期 state-warn]/最新;排序:缺失优先→代码→类型→周期→复权) | app.js:875-887 | 无
5. 已缓存数据表(代码/周期/复权/数量/最早/最新/年龄[分钟前/小时前/天前]/新鲜度/操作;排序默认最新 bar 降序) | app.js:718-759 renderCoverageRows、712-716 fmtAge、1188-1192 | 「暂无已缓存行情数据。」(data.html:79)
6. 新鲜度三态列:正常 / 数据过期(freshness-stale)/ 无数据(freshness-unknown,语义色) | app.js:569-581 freshnessCell | 无
7. 覆盖表行点击 drill-in 打开该标的行情明细(行 title「点击查看 <symbol> <timeframe> (<adjust>)」) | app.js:733-734 | 无
8. 「补数据」按钮:POST /v1/ingest 增量拉取(from=max_ts,幂等;同 `wbot ingest futu` 管线 source=http-api);「拉取中…」→「已更新」,失败恢复并显示原因;成功刷新覆盖表且若明细打开则同步刷新 | app.js:918-940 ingestBars | 无
9. 期权新鲜度表(标的×来源聚合,阈值 4h:标的/来源/最新/年龄/新鲜度/操作;排序默认最新降序) | app.js:769-804 renderOptionsFreshness、1193-1197 | 「暂无期权行情数据。」(data.html:91)
10. 「拉取期权链」按钮:POST /v1/ingest {kind:option} 近端到期(同 `wbot ingest futu-option` 管线);忙态「拉取中…」→「已更新」 | app.js:897-916 ingestOptions | 无
11. 行情明细面板:标题「<symbol> · <timeframe> · <adjust>」+ 8 周期 tab(当前高亮,点切重载)+ 4 指标卡(K 线数/首收盘/末收盘/区间涨跌,涨跌 up/down 语义色) | app.js:1055-1068 renderDetailTabs、1070-1124 renderBarsDetail、1136-1141 | 「从左侧覆盖表选择,或在上方输入代码后加载。」(data.html:107)
12. K 线图 LightweightCharts v4(vendored;蜡烛图实例只建一次,setData 换数据、applyOptions 换主题,保留缩放/平移;库缺失静默降级仅表格) + bars 表(时间/开/高/低/收/涨跌幅[相对前一根]/量) | app.js:990-1019 renderCandlestickChart、964-988 applyChartTheme | 「该周期暂无 K 线。」(data.html:125);加载失败「加载失败,请检查代码与周期。」(app.js:1153)
13. 「刷新覆盖」按钮忙态 + 30s 自动轮询(visibilitychange 隐藏暂停;轮询路径静默吞错,首载/手动刷新仍显示错误)+「更新于」打点 | app.js:1183-1205 | 无
14. 数据页防误触:datacheck 错误 role=alert;datacheck-status aria-live=polite | data.html:57-59 | 无

## Admin(目标 ~8 项,实计 9 项)

1. 集群节点状态 4 卡(进程[版本/PID/运行时长 s/监听地址]/数据库[状态/延迟 ms]/数据管道[运行中/成功/失败]/数据平面[缓存序列/过期序列[+N 无数据]/最新 K 线]);badge 状态机:进程固定「运行中」ok;DB 正常/故障;管道 有失败 warn>进行中>正常>空闲;数据平面 部分过期 warn>无数据 idle>正常 | app.js:518-565 renderCluster、509-513 setBadge | 无
2. 配置表(键/分组/已设置 是|否/更新时间 未设置|时间) | app.js:583-591 renderConfig | 「无配置键。」(admin.html:68)
3. 配置写面:键下拉(来自 GET 元数据)+ 值输入(credentials.* 键自动切 password 类型,值永不回显);空值报「值不能为空」;保存成功「已保存。」并刷新列表 | app.js:602-630 initConfigForm | 无
4. 配置只写不读约定(GET 只回元数据;页面从不请求或显示值) | app.js:595-598、602-630 注释;admin.html:66 说明「仅元数据:配置值只写不读——设置时输入,永不展示/回显」 | 无
5. Telegram 接入向导 3 步说明(BotFather 外链创建机器人/发消息取 chat id/重启 serve --telegram-run 生效) | admin.html:90-95 | 无
6. token 与 chat_ids 两个保存表单(password/text,autocomplete=off;空值报「值不能为空」;保存成功「已保存。」;输入即清空) | app.js:638-667 initTelegramWizard | 无
7. 向导状态提示四态:「已配置:提醒将推送到白名单 chat_ids;重启 serve --telegram-run 生效。」/「token 已配置,还差 chat_ids。」/「chat_ids 已配置,还差 token。」/「未配置:按上面三步填入 token 与 chat_ids。」 | app.js:670-684 renderTelegramWizard | 无
8. telegram 表(token/chat_ids 两键:已配置/更新时间) | app.js:685-688 | 无
9. 「刷新」按钮忙态 + 30s 自动轮询(visibilitychange;cluster/config 为 PG 本地查询)+「更新于」打点 | app.js:692-706 initAdminPage | 无

## 全站共享(目标 ~6 项,实计 7 项)

1. 主题切换:默认跟随系统深浅色(prefers-color-scheme);手动切换持久化 localStorage "wbot-theme";按钮 icon 显示「切换后」主题(浅色界面 🌙 → 点按进深色),aria-label 同步;未手动选择时跟随系统变化 | app.js:9-28 initTheme;style.css:29 html[data-theme="dark"]、52-53 @media(prefers-color-scheme) | 无
2. 导航 5 页(策略=watchlist.html)按 pathname 高亮当前页(/ui/ 与 /ui/index.html 同页归一) | app.js:2687-2695 setActiveNav | 无
3. 统一 fetch 错误处理:网络不可达「cannot reach the server」;API 错误取 {message,action} 约定拼接「msg · action」;非 JSON 响应「unexpected server response」 | app.js:30-60 fetchJSON | 无
4. 通用表格渲染 + 空态切换(table 隐藏 ↔ empty 展示)+ 错误提示条 showError/clearError | app.js:110-126 renderTable、62-70 | 无
5. 通用表头排序组件:data-sort 键绑定,点击切换升/降序,表头 ↕/↑/↓ 指示;数字按值比较、字符串 localeCompare、缺失值 -Infinity 沉底;数字列右对齐镜像(num) | app.js:2079-2118 makeTableSorter、94-108 mirrorNumericColumns | 无
6. 主题切换后图表立即重绘:canvas 绘制闭包缓存 chartCache,切换主题 redrawCharts 逐张重绘;LightweightCharts 实例 applyOptions 不重建(保留缩放) | app.js:948-955 redrawCharts、1021-1047 drawSparkline | 无
7. 展示层格式化约定:时间 ISO→本地「YYYY-MM-DD HH:MM」(非法值原样兜底,排序仍用原始值)、金额 en-US 千分位;图表主题色经 CSS 变量解析(--ok/--down/--border/--muted/--surface/--accent),主题切换重绘生效 | app.js:159-165 fmtTime、153-155/942-944 fmtAccountMoney/fmtNum、964-972 chartTheme | 无
(响应式行为 style.css:max-width:767px 表单列排布/导航换行/输入 min-height 44px/表格 min-width 600px 横向滚动;min-width:1024px 两栏 grid;表格容器 .table-scroll overflow-x:auto — 重构时须保持)

## 核对摘要(实计 vs Sol 预估)

| 页面 | Sol 预估 | 实计 | 差异 |
| --- | --- | --- | --- |
| Dashboard | 12 | 12 | 0 |
| Watchlist | 18 | 21 | +3 |
| Results | 17 | 18 | +1 |
| Data | 13 | 14 | +1 |
| Admin | 8 | 9 | +1 |
| 全站共享 | 6 | 7 | +1 |
| 合计 | 74 | 81 | +7 |

差异原因(Sol 预估偏粗,均属用户可感知功能未单列):
- Watchlist +3:① 深链 #signal-<id> 与 #config-<symbol>-v<N> 是两个独立展开机制(Sol 可能合并为「深链」一项);② 信号→配置版本联动跳转(jumpToConfigVersion)为独立交互;③ 「观察列表计数 N 个标的」独立可感知
- Results +1:详情列表行选中高亮 + 返回列表按钮(Sol 可能并入「详情」一项)
- Data +1:区间语义双档(留空 100 根 / 指定区间 1000 根)与「最近 100 根/指定区间」标签切换(Sol 可能并入表单一项)
- Admin +1:telegram 两键状态表(已配置/更新时间)与四态状态提示分开计
- 全站 +1:展示层格式化约定(时间/金额/图表主题色 CSS 变量解析)独立成项
- 无 Sol 预估多于实计的页面;无遗漏页面(5 页 + 全站共享均已覆盖)
