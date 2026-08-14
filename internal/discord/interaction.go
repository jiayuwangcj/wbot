package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Interaction types Discord sends to the interactions endpoint.
const (
	TypePing             = 1
	TypeApplicationCmd   = 2
	TypeMessageComponent = 3
)

// maxTimestampAge bounds how old X-Signature-Timestamp may be: signature-only
// verification would let a captured request be replayed to place orders.
const maxTimestampAge = 5 * time.Minute

// Verifier checks interaction signatures (Ed25519 over timestamp||body) and
// rejects stale timestamps. The public key comes from the Discord Developer
// Portal and is the endpoint's only trust anchor (anything else is 401).
type Verifier struct {
	publicKey ed25519.PublicKey
}

// NewVerifier parses the hex application public key from the portal.
func NewVerifier(publicKeyHex string) (*Verifier, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("discord: invalid public key")
	}
	return &Verifier{publicKey: ed25519.PublicKey(raw)}, nil
}

// VerifyRequest validates timestamp freshness and the signature over
// timestamp||body (concatenated, no separator — Discord's convention). now is
// injected so tests can pin the clock.
func (v *Verifier) VerifyRequest(timestamp, signatureHex string, body []byte, now time.Time) error {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("discord: invalid signature timestamp")
	}
	if delta := now.Unix() - ts; delta > int64(maxTimestampAge/time.Second) || delta < -int64(maxTimestampAge/time.Second) {
		return errors.New("discord: stale signature timestamp")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(signatureHex))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("discord: invalid signature")
	}
	msg := make([]byte, 0, len(timestamp)+len(body))
	msg = append(msg, timestamp...)
	msg = append(msg, body...)
	if !ed25519.Verify(v.publicKey, msg, sig) {
		return errors.New("discord: signature mismatch")
	}
	return nil
}

// Interaction is a verified webhook payload from Discord.
type Interaction struct {
	ID        string             `json:"id"`
	Type      int                `json:"type"`
	Token     string             `json:"token"`
	ChannelID string             `json:"channel_id"`
	Member    *InteractionMember `json:"member"`
	User      *InteractionUser   `json:"user"`
	Data      *InteractionData   `json:"data"`
	// Message is the message that hosted the pressed component; it carries the
	// message id used to strip buttons after the interaction is handled.
	Message *InteractionMessage `json:"message"`
}

// InteractionMessage is the message context of a component interaction.
type InteractionMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
}

// InteractionMember is the guild member context of a button press.
type InteractionMember struct {
	User InteractionUser `json:"user"`
}

// InteractionUser is the Discord account that pressed the button.
type InteractionUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// InteractionData carries the pressed button's custom_id.
type InteractionData struct {
	Name     string              `json:"name"`
	CustomID string              `json:"custom_id"`
	Options  []InteractionOption `json:"options"`
}

// InteractionOption is one slash-command argument.
type InteractionOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// UserID returns the actor id (message components carry member.user).
func (i *Interaction) UserID() string {
	if i.Member != nil && i.Member.User.ID != "" {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

// Response types for interaction replies (Discord expects an answer within 3s).
const (
	ResponsePong                  = 1
	ResponseChannelMessageWithSrc = 4
	ResponseDeferredChannelMsg    = 5
)

// flagEphemeral makes a CHANNEL_MESSAGE_WITH_SOURCE visible only to the actor.
const flagEphemeral = 64

// Response is the JSON reply to an interaction.
type Response struct {
	Type int           `json:"type"`
	Data *ResponseData `json:"data,omitempty"`
}

// ResponseData is the optional payload of a Response.
type ResponseData struct {
	Content string `json:"content,omitempty"`
	Flags   int    `json:"flags,omitempty"`
}

// Pong answers a PING handshake (Discord endpoint validation).
func Pong() Response { return Response{Type: ResponsePong} }

// EphemeralMessage answers a button press with a private toast.
func EphemeralMessage(content string) Response {
	return Response{Type: ResponseChannelMessageWithSrc, Data: &ResponseData{Content: content, Flags: flagEphemeral}}
}

// DeferredChannelMessage acknowledges a command while work continues.
func DeferredChannelMessage() Response { return Response{Type: ResponseDeferredChannelMsg} }

// WriteResponse encodes resp as the interaction reply.
func WriteResponse(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
