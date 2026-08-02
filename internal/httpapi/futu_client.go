package httpapi

// Shared proto-gateway client for the /v1/futu/* endpoints backed by the
// OpenD protobuf protocol (TCP 11111): /v1/futu/account and /v1/futu/orders.
// One TradeClient per serve process (single connection, mutex-serialized —
// gofutuapi's client is not concurrency-safe), with reconnect-on-drop so a
// gateway that closes idle proto connections does not wedge the endpoints
// until restart (observed live with opend-rs 2026-08-02, PR #96).

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/jiayu/wbot/internal/futu"
)

// protoClient shares one TradeClient across requests, reconnecting once when
// the gateway dropped the connection.
type protoClient struct {
	mu   sync.Mutex
	addr string
	tc   *futu.TradeClient
}

func newProtoClient() *protoClient {
	return newProtoClientAt(FutuAccountAddr())
}

// newProtoClientAt returns a protoClient dialing addr directly (tests).
func newProtoClientAt(addr string) *protoClient {
	return &protoClient{addr: addr}
}

// do runs fn with the shared client (mutex held). When a transport-level
// failure is reported and a client was already open, the stale client is
// abandoned and fn retried once on a fresh connection — the endpoints are
// read-only, so the retry is safe. Do NOT Close() the stale client first:
// gofutuapi's reader goroutine may be inside tryReconnect/connect() replacing
// the same net.Conn (not race-safe); abandoning it parks that goroutine on
// its own idle connection.
func (p *protoClient) do(ctx context.Context, fn func(*futu.TradeClient) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() error {
		if p.tc == nil {
			tc, err := futu.OpenTrade(ctx, p.addr)
			if err != nil {
				return err
			}
			p.tc = tc
		}
		return fn(p.tc)
	}
	err := run()
	if err != nil && p.tc != nil && isConnError(err) {
		p.tc = nil
		err = run()
	}
	return err
}

// isConnError reports transport-level failures (dead socket, EOF, refused,
// reply timeout after the gateway dropped the conn); business errors (bad
// account, trd_env mismatch) never trigger a reconnect. gofutuapi surfaces a
// mid-stream drop as "connection closed" or, when its internal reconnect is
// stuck, "timeout waiting for reply SN N" (observed with opend-rs 2026-08-02).
func isConnError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection closed") || strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "timeout waiting for reply")
}

// FutuAccountAddr returns the gateway proto address: $FUTU_PROTO_ADDR or the
// OpenD default loopback 11111 (doc/FUTU.md). The proto client dials TCP 11111;
// the REST quote/options proxies read $FUTU_GATEWAY_URL (REST 22222) instead —
// the two transports are configured independently, so a REST gateway URL must
// not be fed to the proto dialer.
func FutuAccountAddr() string {
	if v := strings.TrimSpace(os.Getenv("FUTU_PROTO_ADDR")); v != "" {
		return v
	}
	return futu.DefaultProtoAddr
}
