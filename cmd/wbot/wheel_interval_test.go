package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidateWheelInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		wantErr  string
	}{
		{name: "positive", interval: time.Second},
		{name: "zero", interval: 0, wantErr: `invalid -wheel-interval "0s"`},
		{name: "negative", interval: -time.Second, wantErr: `invalid -wheel-interval "-1s"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWheelInterval(tt.interval)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateWheelInterval(%s) = %v; want nil", tt.interval, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateWheelInterval(%s) error = %v; want %q", tt.interval, err, tt.wantErr)
			}
		})
	}
}

func TestServeRejectsInvalidWheelInterval(t *testing.T) {
	for _, value := range []string{"0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			_, stderr, code := captureRun(t, []string{"wbot", "serve", "-wheel-interval=" + value})
			if code != 2 {
				t.Fatalf("run() = %d; want 2 (stderr: %q)", code, stderr)
			}
			if !strings.Contains(stderr, "-wheel-interval") || !strings.Contains(stderr, value) {
				t.Fatalf("stderr = %q; want flag name and received value %q", stderr, value)
			}
		})
	}
}
