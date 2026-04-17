package main

import (
	"net/http/httptest"
	"testing"

	"github.com/jiayu/wbot/internal/httpregister"
	"github.com/jiayu/wbot/internal/master"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"no args", []string{"wbot"}, 2},
		{"help flag", []string{"wbot", "-h"}, 0},
		{"help long", []string{"wbot", "--help"}, 0},
		{"help cmd", []string{"wbot", "help"}, 0},
		{"version flag", []string{"wbot", "-version"}, 0},
		{"version cmd", []string{"wbot", "version"}, 0},
		{"agent poll smoke", []string{"wbot", "agent", "-duration", "1ms", "-interval", "1ms"}, 0},
		{"agent help", []string{"wbot", "agent", "-h"}, 0},
		{"master short run", []string{"wbot", "master", "-duration", "1ms"}, 0},
		{"paper submit", []string{"wbot", "paper", "-symbol", "T.US", "-side", "sell"}, 0},
		{"paper bad side", []string{"wbot", "paper", "-side", "maybe"}, 2},
		{"agent bad flag", []string{"wbot", "agent", "-notaflag"}, 2},
		{"unknown", []string{"wbot", "nope"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestAgentMasterURL(t *testing.T) {
	mem := master.NewMemory()
	srv := httptest.NewServer(httpregister.Handler(mem))
	defer srv.Close()
	if got := run([]string{"wbot", "agent", "-duration", "5ms", "-interval", "1ms", "-master-url", srv.URL}); got != 0 {
		t.Fatalf("run() = %d; want 0", got)
	}
}
