# 2026-08-12 LLM 策略提示词框架固化(模板化 + 产出评估 + 自迭代 + 与 wheel 对照)

- **id**: `2026-08-12-llm-prompt-framework`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(LLM 策略:固化标准提示词框架填参数;参考 write skill 等技能工具评估产出是否符合预期;提示词后续更新由大模型完成;wheel 策略与固化的 Go 实现互相对照查漏补缺)
- **关联**: #37(LLM 策略定时运行)、#36(策略接口统一抽象 wheel+llm)、#35(LLM 注入端点+审核闸门)

## Goal

LLM 策略的决策提示词**工程化**:

1. **固化一套标准提示词框架**(模板骨架固定,只填参数)——同一标的/同一时刻的决策输入结构稳定,输出契约稳定(JSON schema),不随提示词自然语言漂移
2. **产出评估工具化**(参考 write skill / 技能定义机制)——提示词产出的决策信号经校验器评估是否符合预期:输出 schema 校验 + 业务约束校验 + 质量评分;不达标的产出被拦截/标记,不进推送链路
3. **提示词自迭代**——迭代更新提示词也由大模型完成:失败案例(校验不通过/人工否决/与 wheel 对照偏离)→ LLM 分析 → 生成提示词新版本 → 测试集回归 → 通过后升级版本
4. **与 Go wheel 对照查漏补缺**——同一行情快照下,LLM 策略决策 vs 固化 Go 实现(wheel.Evaluate)决策互相对照:方向/数量/理由差异分类记录,双向补漏(LLM 发现 wheel 盲区;wheel 发现 LLM 越界)

## 现状(已探明)

- #35 已有 LLM 审核闸门(6 条规则:方向一致性/经济理由/限价/数量≤1000/数据一致性/系统性)+ 注入端点 `POST /v1/wheel/llm-signal`(输入契约:symbol/direction/quantity/contract/current_price/premium/delta/iv/open_interest/expiry/reason/notes)
- #36 策略接口统一抽象:strategy 注册表目前只有 "wheel" 模板,无 "llm" 模板;Validate 只认 wheel(decisions 无 llm 配置路径)
- #37(排队中):LLM 策略定时运行(deepseek-v4-flash 每 15 分钟)——本任务是其提示词工程化部分,两者共同构成 LLM 策略完整闭环
- wheel.Evaluate:纯 Go 固化实现,价格-目标库存插值 + gap→张数(per-symbol lot_size)+ PUT/CALL/HOLD 三方向 + 候选质量过滤——天然可作为 LLM 决策的对照基准
- 参考:Claude Code skills 机制(write skill 等:结构化定义 + 评估标准 + 可迭代);本项目 .claude/agents/ 角色文件同思路

## 设计(拟)

### 1. 提示词框架(骨架固定 + 参数插槽)

```
LLM 决策提示词 = 固定骨架(framework v1)+ 参数渲染层
骨架固定内容:
  - 角色:期权卖方策略分析师(wheel 语义)
  - 输入区(插槽):行情(price/premium/delta/iv/open_interest/expiry)+ 账户(现金/持仓/库存)+ 策略参数(target_inventory 曲线/lot_size/max_daily_orders/min-max_dte/no_trade_gap)
  - 决策规则:方向三选一(PUT/CALL/HOLD)+ 张数换算(lot_size)+ 硬约束(数量≤max、现金覆盖、DTE 区间、候选质量)
  - 输出契约:固定 JSON schema(复用 #35 契约 + confidence + rationale)
  - 自检清单:方向一致性/经济理由/限价合理性/数量单位/数据新鲜度
参数渲染层:从 watchlist 策略参数 + 行情快照填充插槽,与 Go 实现同一数据源(天然可对照)
```

### 2. 产出评估工具(校验器,参考 write skill 的评估机制)

- **schema 校验**:输出必须是合法 JSON 且字段齐全(缺字段/类型错 → 拒)
- **业务约束校验**:方向∈{PUT,CALL,HOLD};数量=ceil(gap/lot_size) 允许偏差内;DTE 在 [min_dte,max_dte];limit 价格合理(≤/≥ 参考价±容差);数量≤max_daily_orders 对应张数
- **对照校验**:与同快照 wheel.Evaluate 结果对照——方向不一致/数量偏离>容差 → 标记「与基准偏离」并记录理由,进决策日志
- **评估结果落库/落日志**:通过 → 进审核闸门(#35 6 条规则)→ 推送;不通过 → 拦截 + 记录失败案例(进自迭代样本库)
- **工具形态**:内部 Go 校验器(与 wheel 同进程,确定性)+ 评估报告日志

### 3. 提示词自迭代(大模型完成)

- 提示词版本化存储(repo 内 `internal/llmstrategy/prompts/` 或配置,带 version 字段;watchlist 策略参数可选 prompt_version)
- 失败案例样本库:校验拦截 + 人工否决 + 与 wheel 对照偏离 → 结构化案例(输入快照/LLM 产出/预期/实际偏差)
- 迭代循环:积累 N 个案例 → 调 LLM(Claude/codex)分析案例 → 产出新提示词版本 → 用历史案例集回归(全部重跑校验器)→ 回归通过率达标 → 评审 → 升级版本
- 明确约束:提示词变更走评审,不静默替换(决策语义变化影响交易,同 wheel 参数变更纪律)

### 4. 与 Go wheel 对照查漏补缺

- 每标的每轮评估:LLM 决策 + wheel.Evaluate 决策同快照并跑,差异分类:
  - 方向一致数量一致 → 正常
  - 方向一致数量偏差 → 容差内正常,超容差记录
  - 方向不一致 → 最高优先级差异,LLM 必须给出理由(风险 vs 机会差异)
  - wheel HOLD 但 LLM 有单 / 反之 → 记录(LLM 可能发现 wheel 盲区,或 LLM 越界)
- 产出:对照周报/决策日志(每轮一行),人工(老板)可随时查看

## Constraints

- 不改变 #35 审核闸门与推送链路;校验器是前置拦截层,非替代审核
- 输出契约与 #35 llm-signal 输入契约保持兼容(评估通过即能进现有链路)
- wheel.Evaluate 不改(对照基准必须稳定);LLM 策略只读 wheel 输出
- 提示词迭代产物经评审合入(交易语义变更纪律);verify.sh 全绿
- 敏感配置不进仓库;deepseek 走现有凭据渠道(~/.wbot/)
- 与进行中任务无文件重叠(本任务新目录 internal/llmstrategy/;runner 侧改动与库存修复串行)

## Links

- #37 LLM 策略定时运行(deepseek-v4-flash 每 15 分钟,排队中)——本任务与 #37 合并派单或 #37 先行后本任务补提示词工程化
- #36 策略接口抽象(strategy 注册表加 "llm" 模板)+ #35 审核闸门/注入端点
- wheel.Evaluate:internal/wheel/wheel.go(对照基准)
- 参考技能机制:write skill 结构(frontmatter+正文+评估)、.claude/agents/

## State

- **status**: `draft`(2026-08-12 老板指令 → 记录创建)
- **last step**: 需求记录;待并入 #37 排期

## Next

- 与 #37 合并细化(定时器 + 提示词框架 v1 落盘 + 校验器 + 对照日志)→ 排入队列(当前:库存修复 → push-ui 收口 → #37/本任务)→ 派 codex
- JD 现货场景首跑时即产对照基线(LLM 决策 vs wheel 决策首轮样本)
