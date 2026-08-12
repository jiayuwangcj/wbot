package discord

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testKeypair is the fake public-key fixture (项目纪律: 测试全部用假值).
func testKeypair(t *testing.T) (*Verifier, ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVerifier(hex.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	return v, priv, hex.EncodeToString(pub)
}

// signBody signs timestamp||body the way Discord does.
func signBody(t *testing.T, priv ed25519.PrivateKey, ts string, body []byte) string {
	t.Helper()
	msg := append([]byte(ts), body...)
	return hex.EncodeToString(ed25519.Sign(priv, msg))
}

func TestVerifyValidRequest(t *testing.T) {
	v, priv, _ := testKeypair(t)
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"type":3}`)
	if err := v.VerifyRequest(ts, signBody(t, priv, ts, body), body, now); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}

func TestVerifyRejectsTamperedBody(t *testing.T) {
	v, priv, _ := testKeypair(t)
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"type":3,"data":{"custom_id":"wheel:1:yes"}}`)
	tampered := []byte(`{"type":3,"data":{"custom_id":"wheel:1:no"}}`)
	if err := v.VerifyRequest(ts, signBody(t, priv, ts, body), tampered, now); err == nil {
		t.Fatal("tampered body accepted")
	}
}

func TestVerifyRejectsStaleTimestamp(t *testing.T) {
	v, priv, _ := testKeypair(t)
	now := time.Now()
	ts := strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10)
	body := []byte(`{"type":3}`)
	if err := v.VerifyRequest(ts, signBody(t, priv, ts, body), body, now); err == nil {
		t.Fatal("replayed (stale) request accepted")
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	v, _, _ := testKeypair(t)
	_, otherPriv, _ := testKeypair(t)
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"type":3}`)
	if err := v.VerifyRequest(ts, signBody(t, otherPriv, ts, body), body, now); err == nil {
		t.Fatal("signature from another key accepted")
	}
}

func TestVerifyRejectsMalformedInput(t *testing.T) {
	v, priv, _ := testKeypair(t)
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"type":3}`)
	good := signBody(t, priv, ts, body)
	for name, tc := range map[string]struct {
		ts, sig string
	}{
		"bad timestamp": {"nope", good},
		"bad sig hex":   {ts, "zz"},
		"short sig":     {ts, strings.Repeat("ab", 10)},
		"empty sig":     {ts, ""},
	} {
		if err := v.VerifyRequest(tc.ts, tc.sig, body, now); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestNewVerifierRejectsBadHex(t *testing.T) {
	for _, raw := range []string{"", "zz", "abcd"} {
		if _, err := NewVerifier(raw); err == nil {
			t.Fatalf("public key %q accepted", raw)
		}
	}
}

func TestInteractionDecode(t *testing.T) {
	raw := `{"id":"i1","type":3,"token":"tok","channel_id":"c1",
		"member":{"user":{"id":"42","username":"boss"}},
		"data":{"custom_id":"wheel:7:yes"}}`
	var in Interaction
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	if in.Type != TypeMessageComponent || in.ChannelID != "c1" || in.UserID() != "42" {
		t.Fatalf("interaction = %+v", in)
	}
	if in.Data.CustomID != "wheel:7:yes" {
		t.Fatalf("custom_id = %q", in.Data.CustomID)
	}
	// Fallback for top-level user (application commands).
	var app Interaction
	if err := json.Unmarshal([]byte(`{"type":2,"user":{"id":"9"}}`), &app); err != nil {
		t.Fatal(err)
	}
	if app.UserID() != "9" {
		t.Fatalf("top-level user id = %q", app.UserID())
	}
}

func TestResponseShapes(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResponse(rec, Pong())
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("pong = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), `"type":1`) {
		t.Fatalf("pong body = %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	WriteResponse(rec, EphemeralMessage("已记录"))
	if !strings.Contains(rec.Body.String(), `"flags":64`) {
		t.Fatalf("ephemeral body = %s", rec.Body.String())
	}
}

func TestToMarkdown(t *testing.T) {
	in := "<b>📌 US.AAPL · 信号 #7</b>\n候选 <b><code>US.AAPL260815C250000</code></b><br>风险 &lt; 可控 · 5% &amp; 更多"
	want := "**📌 US.AAPL · 信号 #7**\n候选 **`US.AAPL260815C250000`**\n风险 < 可控 · 5% & 更多"
	if got := ToMarkdown(in); got != want {
		t.Fatalf("ToMarkdown:\n got %q\nwant %q", got, want)
	}
}
