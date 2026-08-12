// Package config persists admin configuration to ~/.wbot/wbot.conf (JSON, 0600, atomic writes).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// FileName is the config file inside the ~/.wbot directory.
	FileName = "wbot.conf"
	// MaxValueLen caps a single config value in characters.
	MaxValueLen = 4096
)

var (
	ErrUnknownKey   = errors.New("unknown config key")
	ErrEmptyValue   = errors.New("config value must not be empty")
	ErrValueTooLong = errors.New("config value too long")
)

// Key is one whitelisted configuration key.
type Key struct {
	Name  string // dotted path, e.g. credentials.wechat.appid
	Group string // group prefix, e.g. credentials.wechat
}

// WhitelistedKeys is the accepted key set in canonical order (also the GET listing order).
var WhitelistedKeys = []Key{
	{"credentials.wechat.appid", "credentials.wechat"},
	{"credentials.wechat.secret", "credentials.wechat"},
	{"credentials.wechat.token", "credentials.wechat"},
	{"credentials.schwab.api_key", "credentials.schwab"},
	{"credentials.schwab.account", "credentials.schwab"},
	{"credentials.ibkr.gateway_host", "credentials.ibkr"},
	{"credentials.ibkr.gateway_port", "credentials.ibkr"},
	{"credentials.ibkr.account", "credentials.ibkr"},
	{"credentials.telegram.token", "credentials.telegram"},
	{"credentials.telegram.chat_ids", "credentials.telegram"},
	{"credentials.discord.app_id", "credentials.discord"},
	{"credentials.discord.public_key", "credentials.discord"},
	{"credentials.discord.bot_token", "credentials.discord"},
	{"credentials.discord.channel_id", "credentials.discord"},
	{"system.listen", "system"},
}

// Entry is one key's metadata for the admin API (the value is never exposed).
type Entry struct {
	Key       string  `json:"key"`
	Group     string  `json:"group"`
	Set       bool    `json:"set"`
	UpdatedAt *string `json:"updated_at"` // RFC3339; nil while unset
}

// fileEntry is the on-disk record for one key.
type fileEntry struct {
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

// Store persists config keys to dir/FileName (JSON, 0600, tmp+rename atomic writes).
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open returns a Store rooted at dir, creating dir (0700) if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("config: mkdir %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// OpenDefault returns a Store rooted at ~/.wbot (os.UserHomeDir).
func OpenDefault() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("config: home dir: %w", err)
	}
	return Open(filepath.Join(home, ".wbot"))
}

func (s *Store) path() string { return filepath.Join(s.dir, FileName) }

// List returns metadata for every whitelisted key in canonical order.
func (s *Store) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vals, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(WhitelistedKeys))
	for _, k := range WhitelistedKeys {
		e := Entry{Key: k.Name, Group: k.Group}
		if v, ok := vals[k.Name]; ok {
			e.Set = true
			ts := v.UpdatedAt
			e.UpdatedAt = &ts
		}
		out = append(out, e)
	}
	return out, nil
}

// Set validates key and value against the whitelist limits, then persists atomically.
func (s *Store) Set(key, value string) error {
	if !validKey(key) {
		return ErrUnknownKey
	}
	v := strings.TrimSpace(value)
	if v == "" {
		return ErrEmptyValue
	}
	if len(v) > MaxValueLen {
		return ErrValueTooLong
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vals, err := s.load()
	if err != nil {
		return err
	}
	vals[key] = fileEntry{Value: v, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	return s.save(vals)
}

// Lookup returns the raw value for in-process consumers only (never exposed via the API).
func (s *Store) Lookup(key string) (string, bool, error) {
	if !validKey(key) {
		return "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vals, err := s.load()
	if err != nil {
		return "", false, err
	}
	e, ok := vals[key]
	return e.Value, ok, nil
}

// load reads the file into a key→record map; a missing file means all unset.
func (s *Store) load() (map[string]fileEntry, error) {
	raw, err := os.ReadFile(s.path())
	if os.IsNotExist(err) {
		return map[string]fileEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", s.path(), err)
	}
	var vals map[string]fileEntry
	if err := json.Unmarshal(raw, &vals); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", s.path(), err)
	}
	return vals, nil
}

// save writes vals to a tmp file (0600) and renames it over the config file.
func (s *Store) save(vals map[string]fileEntry) error {
	raw, err := json.MarshalIndent(vals, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, FileName+".tmp*")
	if err != nil {
		return fmt.Errorf("config: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("config: chmod: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("config: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close: %w", err)
	}
	if err := os.Rename(tmpPath, s.path()); err != nil {
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}

func validKey(name string) bool {
	for _, k := range WhitelistedKeys {
		if k.Name == name {
			return true
		}
	}
	return false
}
