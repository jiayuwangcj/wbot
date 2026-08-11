package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetLookupRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const val = "wx-test-appid-1"
	if err := s.Set("credentials.wechat.appid", val); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Lookup("credentials.wechat.appid")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != val {
		t.Fatalf("Lookup = %q, %v; want %q, true", got, ok, val)
	}
}

func TestListUnsetFreshStore(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(WhitelistedKeys) {
		t.Fatalf("len = %d; want %d", len(entries), len(WhitelistedKeys))
	}
	for i, e := range entries {
		if e.Key != WhitelistedKeys[i].Name || e.Group != WhitelistedKeys[i].Group {
			t.Fatalf("entry[%d] = %+v; want key %q group %q", i, e, WhitelistedKeys[i].Name, WhitelistedKeys[i].Group)
		}
		if e.Set || e.UpdatedAt != nil {
			t.Fatalf("entry[%d] = %+v; want unset with nil updated_at", i, e)
		}
	}
}

func TestListReflectsSet(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("credentials.wechat.secret", "wx-test-secret-1"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Entry{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	secret := byKey["credentials.wechat.secret"]
	if !secret.Set || secret.Group != "credentials.wechat" {
		t.Fatalf("secret entry = %+v; want set:true group credentials.wechat", secret)
	}
	if secret.UpdatedAt == nil {
		t.Fatal("secret entry missing updated_at")
	}
	if _, err := time.Parse(time.RFC3339, *secret.UpdatedAt); err != nil {
		t.Fatalf("updated_at %q not RFC3339: %v", *secret.UpdatedAt, err)
	}
	appid := byKey["credentials.wechat.appid"]
	if appid.Set || appid.UpdatedAt != nil {
		t.Fatalf("appid entry = %+v; want untouched", appid)
	}
}

func TestFileMode0600(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("system.listen", "127.0.0.1:8081"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(s.dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o; want 600", info.Mode().Perm())
	}
}

func TestSetRejectsUnknownKey(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"credentials.wechat.foo", "credentials.schwab", "credentials.telegram.foo", "nope", "system.other"} {
		if err := s.Set(key, "x"); err != ErrUnknownKey {
			t.Fatalf("%s: err = %v; want ErrUnknownKey", key, err)
		}
	}
}

// TestTelegramKeysWhitelisted: Telegram 接入向导的两个键(2026-08-11)可写、
// 归组 credentials.telegram、值不回显只可 Lookup(消费侧 internal/telegram)。
func TestTelegramKeysWhitelisted(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("credentials.telegram.token", "123456:ABC-secret"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("credentials.telegram.chat_ids", "111, 222"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Entry{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	for _, key := range []string{"credentials.telegram.token", "credentials.telegram.chat_ids"} {
		e, ok := byKey[key]
		if !ok || !e.Set || e.Group != "credentials.telegram" {
			t.Fatalf("%s entry = %+v; want set with group credentials.telegram", key, e)
		}
	}
	if got, ok, err := s.Lookup("credentials.telegram.token"); err != nil || !ok || got != "123456:ABC-secret" {
		t.Fatalf("token Lookup = %q, %v, %v", got, ok, err)
	}
	if got, ok, err := s.Lookup("credentials.telegram.chat_ids"); err != nil || !ok || got != "111, 222" {
		t.Fatalf("chat_ids Lookup = %q, %v, %v", got, ok, err)
	}
}

func TestSetRejectsEmptyValue(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, val := range []string{"", "   "} {
		if err := s.Set("system.listen", val); err != ErrEmptyValue {
			t.Fatalf("value %q: err = %v; want ErrEmptyValue", val, err)
		}
	}
}

func TestSetRejectsLongValue(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("system.listen", strings.Repeat("a", MaxValueLen+1)); err != ErrValueTooLong {
		t.Fatalf("over-long value: err = %v; want ErrValueTooLong", err)
	}
	if err := s.Set("system.listen", strings.Repeat("a", MaxValueLen)); err != nil {
		t.Fatalf("boundary-length value rejected: %v", err)
	}
}

func TestSetTrimsValue(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("system.listen", "  127.0.0.1:8081  "); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Lookup("system.listen")
	if err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1:8081" {
		t.Fatalf("Lookup = %q; want trimmed value", got)
	}
}

func TestAtomicWriteNoResidue(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.Set("credentials.ibkr.gateway_port", fmt.Sprintf("400%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// Repeated writes overwrite: last one wins.
	got, ok, err := s.Lookup("credentials.ibkr.gateway_port")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "4002" {
		t.Fatalf("Lookup = %q, %v; want 4002, true", got, ok)
	}
	// No tmp file residue: the directory holds exactly the config file.
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name() != FileName {
		t.Fatalf("dir entries = %v; want only %s (no tmp residue)", names, FileName)
	}
}

func TestLookupUnknownKey(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Lookup("nope"); err != nil || ok {
		t.Fatalf("Lookup = _, %v, %v; want false, nil", ok, err)
	}
}

func TestCorruptFileErrors(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(); err == nil {
		t.Fatal("List: want error on corrupt file")
	}
	if err := s.Set("system.listen", "x"); err == nil {
		t.Fatal("Set: want error on corrupt file")
	}
}

func TestOpenCreatesDir0700(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o; want 700", info.Mode().Perm())
	}
}

func TestOpenDefaultUsesHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("system.listen", "127.0.0.1:8082"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Lookup("system.listen"); err != nil || !ok || got != "127.0.0.1:8082" {
		t.Fatalf("Lookup = %q, %v, %v; want 127.0.0.1:8082, true, nil", got, ok, err)
	}
}
