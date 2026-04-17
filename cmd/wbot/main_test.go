package main

import "testing"

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
		{"master note", []string{"wbot", "master"}, 0},
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
