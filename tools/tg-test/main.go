// tg-test 一次性工具(临时):发一条测试推送,验证推送链路。
// 用法: go run ./tools/tg-test [-discord] "文本"
// Telegram 读 ~/.wbot/wbot.conf 的 credentials.telegram.token/chat_ids;
// Discord 模式读 credentials.discord.bot_token/channel_id(与 serve 同源)。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/discord"
	"github.com/jiayu/wbot/internal/telegram"
)

func main() {
	fs := flag.NewFlagSet("tg-test", flag.ContinueOnError)
	discordMode := fs.Bool("discord", false, "send a test embed to the configured Discord channel instead of Telegram")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/tg-test [-discord] <text>")
		os.Exit(2)
	}
	text := fmt.Sprintf("%s\n\n⏱ %s", strings.Join(fs.Args(), " "), time.Now().Format("2006-01-02 15:04:05"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg, err := config.OpenDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if *discordMode {
		sendDiscord(ctx, cfg, text)
		return
	}
	sendTelegram(ctx, cfg, text)
}

func sendTelegram(ctx context.Context, cfg *config.Store, text string) {
	token, tokenSet, err := cfg.Lookup("credentials.telegram.token")
	if err != nil || !tokenSet {
		fmt.Fprintf(os.Stderr, "token: %v (set=%v)\n", err, tokenSet)
		os.Exit(1)
	}
	chatRaw, chatSet, err := cfg.Lookup("credentials.telegram.chat_ids")
	if err != nil || !chatSet {
		fmt.Fprintf(os.Stderr, "chat_ids: %v (set=%v)\n", err, chatSet)
		os.Exit(1)
	}
	chatIDs, err := telegram.ParseChatIDs(chatRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chat_ids parse: %v\n", err)
		os.Exit(1)
	}
	tg, err := telegram.New(token, strings.TrimSpace(os.Getenv("TELEGRAM_API_BASE_URL")), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}
	for id := range chatIDs {
		if err := tg.SendMessage(ctx, fmt.Sprintf("%d", id), text, nil); err != nil {
			fmt.Fprintf(os.Stderr, "send to %d: %v\n", id, err)
			os.Exit(1)
		}
		fmt.Printf("sent to %d\n", id)
	}
	fmt.Println("ok")
}

func sendDiscord(ctx context.Context, cfg *config.Store, text string) {
	token, tokenSet, err := cfg.Lookup("credentials.discord.bot_token")
	if err != nil || !tokenSet {
		fmt.Fprintf(os.Stderr, "bot_token: %v (set=%v)\n", err, tokenSet)
		os.Exit(1)
	}
	channel, channelSet, err := cfg.Lookup("credentials.discord.channel_id")
	if err != nil || !channelSet {
		fmt.Fprintf(os.Stderr, "channel_id: %v (set=%v)\n", err, channelSet)
		os.Exit(1)
	}
	dc, err := discord.New(token, strings.TrimSpace(os.Getenv("DISCORD_API_BASE_URL")), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}
	msg := discord.Message{Embeds: []discord.Embed{{
		Title:       "tg-test",
		Description: text,
		Color:       discord.ColorApprove,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}}}
	if err := dc.CreateMessage(ctx, channel, msg); err != nil {
		fmt.Fprintf(os.Stderr, "send to %s: %v\n", channel, err)
		os.Exit(1)
	}
	fmt.Printf("sent embed to channel %s\n", channel)
	fmt.Println("ok")
}
