package main

// 开盘准备状态推送(2026-08-18 老板指令):serve 启动后立即 + HK/US 开盘前
// 各推一次 Telegram 状态报告(连接/watch 标的/账户资金/单进程组件视图)。
// 纯查询+只读,不触碰 wheel ALERT / datacheck / discord 既有推送路径。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/datacheck"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/httpapi"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/notify"
	"github.com/jiayu/wbot/internal/telegram"
	"github.com/jiayu/wbot/internal/watchlist"
)

// marketOpenOuterRule 分隔报告主块(与 alertOuterRule 同字符,保持推送观感一致)。
const marketOpenOuterRule = "━━━━━━━━━━━━━━━━━━━━"

// errMarketOpenNotConfigured 是 Telegram 凭据缺失时的一次性跳过原因。
var errMarketOpenNotConfigured = errors.New("not configured (set credentials.telegram.token and credentials.telegram.chat_ids in ~/.wbot/wbot.conf, then restart serve)")

// startMarketOpenScheduler 在 serve 启动后立即推送一次,并每天在 hkAt/usAt
// (本地 HH:MM,serve 容器 TZ=Asia/Shanghai)各推一次。凭据复用 wbot.conf
// telegram 配置;未配置时 log once 后跳过(与 startTelegramScheduler 一致)。
// 报告是尽力而为:任一依赖失败都渲染进文本,推送失败只 log stderr 不 crash。
func startMarketOpenScheduler(ctx context.Context, database *sql.DB, meta httpapi.ProcessMeta, env futu.Env, hkAt, usAt string) {
	sender, err := marketOpenNotifierFromConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "market-open: %v\n", err)
		return
	}
	push := func(runCtx context.Context, now time.Time) {
		data := collectMarketOpenReport(runCtx, database, meta, env, now)
		if err := sender.Send(runCtx, limitMessage(formatMarketOpenReport(data), 1800)); err != nil {
			fmt.Fprintf(os.Stderr, "market-open: push: %v\n", err)
		}
	}
	push(ctx, time.Now())
	if hkHour, hkMin, err := parseDailyTime(hkAt); err != nil {
		fmt.Fprintf(os.Stderr, "market-open: -prep-at-hk: %v\n", err)
	} else {
		go datacheck.RunDaily(ctx, hkHour, hkMin, push)
	}
	if usHour, usMin, err := parseDailyTime(usAt); err != nil {
		fmt.Fprintf(os.Stderr, "market-open: -prep-at-us: %v\n", err)
	} else {
		go datacheck.RunDaily(ctx, usHour, usMin, push)
	}
}

// marketOpenNotifierFromConfig 从 wbot.conf 读 telegram 凭据并构造每个 chat 的
// notify.Sender(复用 openTelegramConfig 模式);任一 chat 失败不影响其余。
func marketOpenNotifierFromConfig() (notify.Sender, error) {
	cfg, err := openTelegramConfig()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	token, tokenSet, err := cfg.Lookup("credentials.telegram.token")
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	chatRaw, chatSet, err := cfg.Lookup("credentials.telegram.chat_ids")
	if err != nil {
		return nil, fmt.Errorf("chat_ids: %w", err)
	}
	if !tokenSet || !chatSet || strings.TrimSpace(token) == "" || strings.TrimSpace(chatRaw) == "" {
		return nil, errMarketOpenNotConfigured
	}
	chatIDs, err := telegram.ParseChatIDs(chatRaw)
	if err != nil {
		return nil, err
	}
	if len(chatIDs) == 0 {
		return nil, errors.New("chat_ids: no valid chat id")
	}
	baseURL := strings.TrimSpace(os.Getenv("TELEGRAM_API_BASE_URL"))
	var senders notify.MultiSender
	for id := range chatIDs {
		s, err := notify.NewTelegram(token, strconv.FormatInt(id, 10), baseURL, nil)
		if err != nil {
			return nil, err
		}
		senders = append(senders, s)
	}
	return senders, nil
}

// marketOpenReportData 是一次报告的纯数据载荷(组装与渲染分离,便于单测)。
type marketOpenReportData struct {
	GeneratedAt time.Time
	GatewayOK   bool
	GatewayDesc string
	DBOK        bool
	DBDesc      string
	LLMOK       bool
	LLMDesc     string
	WatchErr    string
	Watch       []marketOpenWatchItem
	Account     marketOpenAccount
	Cluster     marketOpenCluster
}

type marketOpenWatchItem struct {
	Symbol             string
	Strategy           string
	VersionText        string
	ExecutionStatus    string
	InvalidationReason string
	ParamsSummary      string
}

type marketOpenAccount struct {
	OK            bool
	Env           string
	AccID         uint64
	Err           string
	AvailableCash float64
	Cash          float64
	MarketVal     float64
	TotalAssets   float64
	Positions     []marketOpenPosition
}

type marketOpenPosition struct {
	Symbol    string
	Qty       float64
	MarketVal float64
	PL        float64
}

type marketOpenCluster struct {
	OK                bool
	Err               string
	Version           string
	UptimeSeconds     float64
	DBOK              bool
	DBLatencyMS       float64
	PipelineRunning   int64
	PipelineSucceeded int64
	PipelineFailed    int64
	BarsSymbols       int
	BarsStale         int
	OptionsFresh      int
	OptionsStale      int
}

// collectMarketOpenReport 组装一次报告:每个依赖失败都降级为文本块,不让
// 单个依赖拖垮整份报告。
func collectMarketOpenReport(ctx context.Context, database *sql.DB, meta httpapi.ProcessMeta, env futu.Env, now time.Time) marketOpenReportData {
	d := marketOpenReportData{GeneratedAt: now}
	d.GatewayOK, d.GatewayDesc = marketOpenGatewayStatus(ctx)
	d.DBOK, d.DBDesc = marketOpenDBStatus(ctx, database)
	d.LLMOK, d.LLMDesc = marketOpenLLMStatus()

	items, err := watchlist.List(ctx, database)
	if err != nil {
		d.WatchErr = err.Error()
	} else {
		d.Watch = make([]marketOpenWatchItem, 0, len(items))
		for _, it := range items {
			d.Watch = append(d.Watch, marketOpenWatchItem{
				Symbol:             it.Symbol,
				Strategy:           it.Strategy,
				VersionText:        versionText(it.ConfigVersion),
				ExecutionStatus:    it.ExecutionStatus,
				InvalidationReason: it.InvalidationReason,
				ParamsSummary:      summarizeParams(it.Params),
			})
		}
	}

	d.Account = collectMarketOpenAccount(ctx, env)
	d.Cluster = collectMarketOpenCluster(ctx, database, meta)
	return d
}

func marketOpenGatewayStatus(ctx context.Context) (bool, string) {
	client := futu.NewClient(resolveFutuGateway(""))
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	st, err := client.Status(sctx)
	if err != nil {
		return false, "不可达: " + err.Error()
	}
	parts := []string{st.Health}
	if st.QotLogined {
		parts = append(parts, "行情已登录")
	} else {
		parts = append(parts, "行情未登录")
	}
	if st.TrdLogined {
		parts = append(parts, "交易已登录")
	} else {
		parts = append(parts, "交易未登录")
	}
	return true, strings.Join(parts, " · ")
}

func marketOpenDBStatus(ctx context.Context, database *sql.DB) (bool, string) {
	if database == nil {
		return false, "不可达: nil db"
	}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	err := database.PingContext(pctx)
	if err != nil {
		return false, "不可达: " + err.Error()
	}
	return true, fmt.Sprintf("ok (%.1fms)", time.Since(start).Seconds()*1000)
}

func marketOpenLLMStatus() (bool, string) {
	if strings.TrimSpace(os.Getenv("LLM_BASE_URL")) == "" || strings.TrimSpace(os.Getenv("LLM_API_KEY")) == "" || strings.TrimSpace(os.Getenv("LLM_MODEL")) == "" {
		return false, "未配置 (LLM_BASE_URL/LLM_API_KEY/LLM_MODEL)"
	}
	return true, "已配置 · " + strings.TrimSpace(os.Getenv("LLM_MODEL"))
}

func collectMarketOpenAccount(ctx context.Context, env futu.Env) marketOpenAccount {
	var out marketOpenAccount
	actx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	snap, err := httpapi.NewFutuAccounter().Account(actx, env, 0)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	out.OK = true
	out.Env = snap.Env
	out.AccID = snap.AccID
	out.AvailableCash = snap.Funds.AvailableCash
	out.Cash = snap.Funds.Cash
	out.MarketVal = snap.Funds.MarketVal
	out.TotalAssets = snap.Funds.TotalAssets
	out.Positions = make([]marketOpenPosition, 0, len(snap.Positions))
	for _, p := range snap.Positions {
		out.Positions = append(out.Positions, marketOpenPosition{Symbol: p.Symbol, Qty: p.Qty, MarketVal: p.MarketVal, PL: p.PL})
	}
	return out
}

func collectMarketOpenCluster(ctx context.Context, database *sql.DB, meta httpapi.ProcessMeta) marketOpenCluster {
	out := marketOpenCluster{Version: meta.Version, UptimeSeconds: time.Since(meta.StartedAt).Seconds()}
	if database == nil {
		out.Err = "db ping: nil db"
		return out
	}
	store := httpapi.NewDBStore(database)
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := store.Ping(pctx); err != nil {
		out.Err = "db ping: " + err.Error()
		return out
	}
	out.OK = true
	out.DBLatencyMS = time.Since(start).Seconds() * 1000
	counts, err := store.RunStatusCounts(ctx)
	if err != nil {
		out.Err = "pipeline counts: " + err.Error()
		return out
	}
	out.PipelineRunning, out.PipelineSucceeded, out.PipelineFailed = counts.Running, counts.Succeeded, counts.Failed
	coverage, err := store.BarCoverage(ctx)
	if err != nil {
		out.Err = "bars coverage: " + err.Error()
		return out
	}
	now := time.Now()
	symbols := map[string]bool{}
	for _, c := range coverage {
		symbols[c.Symbol] = true
		if ingest.JudgeFreshness(c.MaxTs, now, ingest.MaxAgeForTimeframe(c.Timeframe)) == ingest.Stale {
			out.BarsStale++
		}
	}
	out.BarsSymbols = len(symbols)
	opts, err := store.OptionFreshness(ctx)
	if err != nil {
		out.Err = "option freshness: " + err.Error()
		return out
	}
	for _, o := range opts {
		if ingest.JudgeFreshness(o.MaxTs, now, ingest.MaxAgeForOptionSource(o.Source)) == ingest.Stale {
			out.OptionsStale++
		} else {
			out.OptionsFresh++
		}
	}
	return out
}

// formatMarketOpenReport 渲染纯文本报告(notify.Sender 不带 parse_mode,
// 故用 emoji + 分隔线排版,不用 HTML)。
func formatMarketOpenReport(d marketOpenReportData) string {
	var b strings.Builder
	b.WriteString("📋 开盘准备状态\n")
	b.WriteString("时间: " + d.GeneratedAt.Format("2006-01-02 15:04:05") + "\n")
	b.WriteString(marketOpenOuterRule + "\n")

	b.WriteString("🔌 连接情况\n")
	b.WriteString("- futu 网关: " + connMark(d.GatewayOK) + " " + d.GatewayDesc + "\n")
	b.WriteString("- DB: " + connMark(d.DBOK) + " " + d.DBDesc + "\n")
	b.WriteString("- LLM 审核: " + connMark(d.LLMOK) + " " + d.LLMDesc + "\n")

	b.WriteString("👀 Watch 标的")
	switch {
	case d.WatchErr != "":
		b.WriteString("\n- 查询失败: " + d.WatchErr + "\n")
	case len(d.Watch) == 0:
		b.WriteString(" (空)\n")
	default:
		fmt.Fprintf(&b, " (%d)\n", len(d.Watch))
		for _, w := range d.Watch {
			fmt.Fprintf(&b, "- %s · %s · v%s · %s\n", w.Symbol, strategyName(w.Strategy), w.VersionText, statusText(w.ExecutionStatus))
			if s := w.ParamsSummary; s != "" {
				b.WriteString("  " + s + "\n")
			}
			if w.InvalidationReason != "" {
				b.WriteString("  ⚠️ " + w.InvalidationReason + "\n")
			}
		}
	}

	b.WriteString("💰 账户/资金")
	switch {
	case !d.Account.OK:
		b.WriteString("\n- 查询失败: " + d.Account.Err + "\n")
	default:
		fmt.Fprintf(&b, " (%s)\n", d.Account.Env)
		if d.Account.AccID != 0 {
			fmt.Fprintf(&b, "- 账户: %d\n", d.Account.AccID)
		}
		fmt.Fprintf(&b, "- 可用资金: %s\n", moneyText(d.Account.AvailableCash))
		fmt.Fprintf(&b, "- 现金: %s · 市值: %s · 总资产: %s\n", moneyText(d.Account.Cash), moneyText(d.Account.MarketVal), moneyText(d.Account.TotalAssets))
		if len(d.Account.Positions) == 0 {
			b.WriteString("- 持仓: 0 笔\n")
		} else {
			fmt.Fprintf(&b, "- 持仓 %d 笔\n", len(d.Account.Positions))
			for i, p := range d.Account.Positions {
				if i == 10 {
					fmt.Fprintf(&b, "  …其余 %d 笔\n", len(d.Account.Positions)-10)
					break
				}
				fmt.Fprintf(&b, "  %s %s 股 市值 %s 盈亏 %s\n", p.Symbol, quantityText(p.Qty), moneyText(p.MarketVal), moneyText(p.PL))
			}
		}
	}

	b.WriteString("🖥 服务集群(单进程组件)\n")
	if !d.Cluster.OK {
		b.WriteString("- 查询失败: " + d.Cluster.Err + "\n")
	} else {
		fmt.Fprintf(&b, "- 进程: v%s · 运行 %s\n", d.Cluster.Version, uptimeText(d.Cluster.UptimeSeconds))
		fmt.Fprintf(&b, "- DB: %s\n", dbMark(d.Cluster.DBLatencyMS))
		fmt.Fprintf(&b, "- 数据管道: 运行 %d / 成功 %d / 失败 %d\n", d.Cluster.PipelineRunning, d.Cluster.PipelineSucceeded, d.Cluster.PipelineFailed)
		fmt.Fprintf(&b, "- 数据面: bars 标的 %d(过期 %d) · 期权新鲜 %d / 过期 %d\n", d.Cluster.BarsSymbols, d.Cluster.BarsStale, d.Cluster.OptionsFresh, d.Cluster.OptionsStale)
	}

	b.WriteString(marketOpenOuterRule + "\n")
	return strings.TrimSpace(b.String())
}

func connMark(ok bool) string {
	if ok {
		return "✅"
	}
	return "❌"
}

func dbMark(latencyMS float64) string {
	return fmt.Sprintf("✅ (%.1fms)", latencyMS)
}

func strategyName(s string) string {
	switch strings.TrimSpace(s) {
	case "llm":
		return "LLM 策略"
	case "wheel":
		return "固化策略"
	}
	if s == "" {
		return "未设置"
	}
	return s
}

func statusText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "未运行"
	}
	return s
}

func versionText(v *int) string {
	if v == nil || *v <= 0 {
		return "-"
	}
	return strconv.Itoa(*v)
}

// summarizeParams 渲染参数的紧凑一行(wheel 关键参数 + 结构参数条数),稳定
// 排序保证文本可测;过长截断。
func summarizeParams(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := params[k]
		switch val := v.(type) {
		case nil:
			continue
		case string:
			if val == "" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%s", k, val))
		case float64:
			parts = append(parts, fmt.Sprintf("%s=%v", k, trimFloat(val)))
		case bool:
			parts = append(parts, fmt.Sprintf("%s=%v", k, val))
		default:
			switch vv := v.(type) {
			case []any:
				parts = append(parts, fmt.Sprintf("%s=[%d项]", k, len(vv)))
			case map[string]any:
				parts = append(parts, fmt.Sprintf("%s={%d键}", k, len(vv)))
			default:
				parts = append(parts, fmt.Sprintf("%s=%v", k, val))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := strings.Join(parts, " ")
	if r := []rune(out); len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return out
}

func trimFloat(v float64) float64 {
	if v == math.Trunc(v) {
		return v
	}
	return math.Round(v*10000) / 10000
}

func moneyText(v float64) string {
	neg := v < 0
	abs := math.Abs(v)
	whole := int64(abs)
	cents := int(math.Round((abs - float64(whole)) * 100))
	if cents == 100 {
		whole++
		cents = 0
	}
	out := commaInt(whole) + "." + fmt.Sprintf("%02d", cents)
	if neg {
		out = "-" + out
	}
	return out
}

func quantityText(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func uptimeText(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Minute).String()
}
