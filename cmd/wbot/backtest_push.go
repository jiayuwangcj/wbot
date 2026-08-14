package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiayu/wbot/internal/backtestpush"
	"github.com/jiayu/wbot/internal/backtestreport"
	"github.com/jiayu/wbot/internal/discord"
)

func pushBacktestReport(ctx context.Context, report *backtestreport.Report) (string, error) {
	cfg, err := openTelegramConfig()
	if err != nil {
		return "", err
	}
	token, tokenSet, err := cfg.Lookup("credentials.discord.bot_token")
	if err != nil {
		return "", err
	}
	channelID, channelSet, err := cfg.Lookup("credentials.discord.channel_id")
	if err != nil {
		return "", err
	}
	if !tokenSet || !channelSet || strings.TrimSpace(token) == "" || strings.TrimSpace(channelID) == "" {
		return "", errors.New("discord 未配置；请在 ~/.wbot/wbot.conf（或 WBOT_CONFIG_DIR/wbot.conf）设置 credentials.discord.bot_token 和 credentials.discord.channel_id")
	}
	client, err := discord.New(strings.TrimSpace(token), strings.TrimSpace(os.Getenv("DISCORD_API_BASE_URL")), nil)
	if err != nil {
		return "", err
	}
	stateDir, err := backtestPushStateDir()
	if err != nil {
		return "", err
	}
	return backtestpush.Push(ctx, client, strings.TrimSpace(channelID), stateDir, report)
}

func backtestPushStateDir() (string, error) {
	root := strings.TrimSpace(os.Getenv("WBOT_CONFIG_DIR"))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".wbot")
	}
	return filepath.Join(root, "backtest-push", "discord"), nil
}
