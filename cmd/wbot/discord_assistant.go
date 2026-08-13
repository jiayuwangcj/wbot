package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	claudeAskTimeout       = 180 * time.Second
	discordAssistantMaxLen = 1900
)

// assistant answers one stateless question.
type assistant interface {
	Ask(ctx context.Context, prompt string) (string, error)
}

// claudeAssistant invokes Claude Code in non-interactive print mode.
type claudeAssistant struct {
	cliPath string
	apiKey  string
	timeout time.Duration
}

func newClaudeAssistant(cliPath, apiKey string) *claudeAssistant {
	cliPath = strings.TrimSpace(cliPath)
	if cliPath == "" {
		cliPath = "claude"
	}
	return &claudeAssistant{cliPath: cliPath, apiKey: strings.TrimSpace(apiKey), timeout: claudeAskTimeout}
}

func (a *claudeAssistant) Ask(ctx context.Context, prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", errors.New("问题不能为空")
	}
	timeout := a.timeout
	if timeout <= 0 {
		timeout = claudeAskTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// --max-turns:真实问题需要工具调用(读文件等),默认回合数不足会
	// Reached max turns(实测「0700在关注列表吗」3 回合即报错)。
	cmd := exec.CommandContext(callCtx, a.cliPath, "-p", prompt, "--max-turns", "20")
	cmd.Env = os.Environ()
	if a.apiKey != "" {
		for i := len(cmd.Env) - 1; i >= 0; i-- {
			if strings.HasPrefix(cmd.Env[i], "ANTHROPIC_API_KEY=") {
				cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
			}
		}
		cmd.Env = append(cmd.Env, "ANTHROPIC_API_KEY="+a.apiKey)
	}
	configureAssistantCommand(cmd)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if callCtx.Err() != nil {
		return "", fmt.Errorf("claude CLI 超时: %w", callCtx.Err())
	}
	if err != nil {
		reason := strings.TrimSpace(string(out))
		if a.apiKey != "" {
			reason = strings.ReplaceAll(reason, a.apiKey, "[REDACTED]")
		}
		if len([]rune(reason)) > 300 {
			reason = string([]rune(reason)[:300]) + "…"
		}
		if reason == "" {
			reason = err.Error()
		}
		return "", fmt.Errorf("claude CLI: %s", reason)
	}
	reply := strings.TrimSpace(string(out))
	if reply == "" {
		return "", errors.New("claude CLI 返回空回答")
	}
	return reply, nil
}

func truncateAssistantReply(reply string) string {
	const suffix = "\n\n（回答过长，已截断）"
	runes := []rune(strings.TrimSpace(reply))
	if len(runes) <= discordAssistantMaxLen {
		return string(runes)
	}
	keep := discordAssistantMaxLen - len([]rune(suffix))
	return string(runes[:keep]) + suffix
}
