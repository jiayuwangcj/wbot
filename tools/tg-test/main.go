// tg-test 一次性工具(临时):发一条 Telegram 测试推送,验证推送链路。
// 用法: go run ./tools/tg-test "文本"
// 读 ~/.wbot/wbot.conf 的 credentials.telegram.token/chat_ids(与 serve 同源)。
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/telegram"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/tg-test <text>")
		os.Exit(2)
	}
	cfg, err := config.OpenDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
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
	text := fmt.Sprintf("%s\n\n⏱ %s", strings.Join(os.Args[1:], " "), time.Now().Format("2006-01-02 15:04:05"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for id := range chatIDs {
		if err := tg.SendMessage(ctx, fmt.Sprintf("%d", id), text, nil); err != nil {
			fmt.Fprintf(os.Stderr, "send to %d: %v\n", id, err)
			os.Exit(1)
		}
		fmt.Printf("sent to %d\n", id)
	}
	fmt.Println("ok")
}
