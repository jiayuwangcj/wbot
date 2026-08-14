// Package backtestpush projects backtest reports into deterministic Discord
// embeds and records successful deliveries by report ID.
package backtestpush

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiayu/wbot/internal/backtestreport"
	"github.com/jiayu/wbot/internal/discord"
)

const (
	StatusSent        = "sent"
	StatusAlreadySent = "already_sent"
	colorResearch     = 0xF2BD5C
)

// Sender is the subset of the Discord client used by report delivery.
type Sender interface {
	CreateMessage(context.Context, string, discord.Message) error
}

// Push sends one report at most once per state directory. The durable marker
// is written only after Discord accepts the message, so a failed attempt can
// be retried with the same report ID. A deterministic enforced Discord nonce
// also closes the short response-lost window between the POST and marker write.
func Push(ctx context.Context, sender Sender, channelID, stateDir string, report *backtestreport.Report) (string, error) {
	if sender == nil {
		return "", errors.New("backtest push: nil Discord sender")
	}
	if strings.TrimSpace(channelID) == "" {
		return "", errors.New("backtest push: Discord channel ID is required")
	}
	if report == nil || strings.TrimSpace(report.ReportID) == "" {
		return "", errors.New("backtest push: report ID is required")
	}
	if strings.ContainsAny(report.ReportID, `/\\`) {
		return "", errors.New("backtest push: invalid report ID")
	}
	if strings.TrimSpace(stateDir) == "" {
		return "", errors.New("backtest push: state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("backtest push: create state directory: %w", err)
	}

	lock, err := acquireFileLock(filepath.Join(stateDir, report.ReportID+".lock"))
	if err != nil {
		return "", fmt.Errorf("backtest push: lock report: %w", err)
	}
	defer lock.release()

	marker := filepath.Join(stateDir, report.ReportID+".sent")
	if _, err := os.Stat(marker); err == nil {
		return StatusAlreadySent, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("backtest push: inspect sent marker: %w", err)
	}

	message, err := Message(report)
	if err != nil {
		return "", err
	}
	if err := sender.CreateMessage(ctx, strings.TrimSpace(channelID), message); err != nil {
		return "", fmt.Errorf("backtest push: send Discord report: %w", err)
	}
	if err := writeMarker(marker, report.ReportID); err != nil {
		return "", err
	}
	return StatusSent, nil
}

// Message is the deterministic Discord projection of the report's JSON
// source of truth. Risk text is copied in full; it is never shortened into a
// misleading status. Reports exceeding Discord's documented embed limits fail
// before the network call instead of being silently truncated.
func Message(report *backtestreport.Report) (discord.Message, error) {
	if report == nil || strings.TrimSpace(report.ReportID) == "" {
		return discord.Message{}, errors.New("backtest push: report ID is required")
	}
	risk := strings.Join(report.Risk, "\n")
	description := "风险提示（完整）"
	if risk != "" {
		description += "\n" + risk
	}
	if len([]rune(description)) > 4096 {
		return discord.Message{}, errors.New("backtest push: complete risk text exceeds Discord embed description limit")
	}

	premiumResult := report.Identity.CapabilityStatus + " · N/A"
	if report.Result.NetReturnPct != nil {
		premiumResult = percent(*report.Result.NetReturnPct)
	}
	if report.Result.AnnualizedReturnPct != nil {
		premiumResult += " · 年化 " + percent(*report.Result.AnnualizedReturnPct)
	}
	if report.Result.GrossReturnPct != nil {
		premiumResult += " · 毛 " + percent(*report.Result.GrossReturnPct)
	}
	realizedResult := report.Identity.CapabilityStatus + " · N/A"
	if report.Result.RealizedReturnPct != nil {
		realizedResult = percent(*report.Result.RealizedReturnPct)
		if report.Result.RealizedAnnualizedReturnPct != nil {
			realizedResult += " · 年化 " + percent(*report.Result.RealizedAnnualizedReturnPct)
		}
	}
	coverage := "N/A"
	if report.DataQuality.ValidCoverageRatio != nil {
		coverage = fmt.Sprintf("%s · %d/%d bars", percent(*report.DataQuality.ValidCoverageRatio), report.DataQuality.ReadyBarCount, report.DataQuality.TotalBarCount)
	}
	feeStatus := "未计入"
	if report.Result.CostModel.FeesIncluded {
		feeStatus = "已计入"
	}
	stopReason := "单次回测完成"
	if report.Train != nil {
		stopReason = report.Train.StopReason
		if report.Train.StopDetail != "" {
			stopReason += " · " + report.Train.StopDetail
		}
	}
	title := "回测报告 · " + report.Identity.Symbol
	author := "wbot · RESEARCH_ONLY"
	footer := report.ReportID
	if len([]rune(title)) > 256 || len([]rune(author)) > 256 || len([]rune(footer)) > 2048 {
		return discord.Message{}, errors.New("backtest push: Discord embed identity exceeds limit")
	}
	fields := []discord.EmbedField{
		{Name: "标的", Value: report.Identity.Symbol, Inline: true},
		{Name: "数据窗口", Value: report.Identity.DataWindow.From + " — " + report.Identity.DataWindow.To + fmt.Sprintf(" · 本金 %.2f · 期末 %.2f", report.InitialCash, valueOrZero(report.Result.FinalEquityAmount))},
		{Name: "权利金净额口径", Value: premiumResult, Inline: true},
		{Name: "已实现口径", Value: realizedResult, Inline: true},
		{Name: "有效覆盖率", Value: coverage, Inline: true},
		{Name: "费用", Value: fmt.Sprintf("%s %.2f · 损耗 %.2f%% · %s", report.Identity.Currency, report.Result.CostModel.TotalFeesAmount, report.Result.CostDragPct*100, feeStatus), Inline: true},
		{Name: "最大回撤", Value: percent(report.Result.MaxDrawdownPct), Inline: true},
		{Name: "停止原因", Value: stopReason},
	}
	totalRunes := len([]rune(title)) + len([]rune(author)) + len([]rune(description)) + len([]rune(footer))
	for _, field := range fields {
		if strings.TrimSpace(field.Value) == "" {
			return discord.Message{}, fmt.Errorf("backtest push: empty Discord field %q", field.Name)
		}
		if len([]rune(field.Name)) > 256 || len([]rune(field.Value)) > 1024 {
			return discord.Message{}, fmt.Errorf("backtest push: Discord field %q exceeds embed limit", field.Name)
		}
		totalRunes += len([]rune(field.Name)) + len([]rune(field.Value))
	}
	if totalRunes > 6000 {
		return discord.Message{}, errors.New("backtest push: complete report exceeds Discord embed limit")
	}

	color := colorResearch
	if report.Identity.CapabilityStatus == "DATA_BLOCKED" {
		color = discord.ColorAlert
	}
	return discord.Message{
		Embeds: []discord.Embed{{
			Author: &discord.EmbedAuthor{Name: author},
			Title:  title, Description: description,
			Color: color, Fields: fields, Footer: &discord.EmbedFooter{Text: footer},
		}},
		Nonce: nonce(report.ReportID), EnforceNonce: true,
	}, nil
}

func valueOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func nonce(reportID string) string {
	digest := sha256.Sum256([]byte(reportID))
	return hex.EncodeToString(digest[:])[:25]
}

func percent(value float64) string { return fmt.Sprintf("%.2f%%", value*100) }

func writeMarker(path, reportID string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sent-*")
	if err != nil {
		return fmt.Errorf("backtest push: create sent marker: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("backtest push: protect sent marker: %w", err)
	}
	if _, err := fmt.Fprintln(tmp, reportID); err != nil {
		tmp.Close()
		return fmt.Errorf("backtest push: write sent marker: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("backtest push: sync sent marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("backtest push: close sent marker: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("backtest push: commit sent marker: %w", err)
	}
	return nil
}
