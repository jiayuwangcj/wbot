package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestClaudeAssistantFixture(t *testing.T) {
	a := newClaudeAssistant("testdata/fake-claude.sh", "fake-api-key")
	reply, err := a.Ask(context.Background(), "api-key")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "key:fake-api-key" {
		t.Fatalf("reply = %q", reply)
	}
}

func TestClaudeAssistantFailureAndTimeout(t *testing.T) {
	a := newClaudeAssistant("testdata/fake-claude.sh", "")
	if _, err := a.Ask(context.Background(), "fail"); err == nil || !strings.Contains(err.Error(), "fixture failure") {
		t.Fatalf("failure err = %v", err)
	}
	a.timeout = 20 * time.Millisecond
	if _, err := a.Ask(context.Background(), "timeout"); err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("timeout err = %v", err)
	}
}

func TestTruncateAssistantReply(t *testing.T) {
	got := truncateAssistantReply(strings.Repeat("问", 1901))
	if len([]rune(got)) != discordAssistantMaxLen || !strings.HasSuffix(got, "（回答过长，已截断）") {
		t.Fatalf("truncated length = %d suffix = %q", len([]rune(got)), got[len(got)-40:])
	}
	if got := truncateAssistantReply(" short "); got != "short" {
		t.Fatalf("short reply = %q", got)
	}
}
