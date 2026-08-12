package httpapi

// Shared proto-gateway client for the /v1/futu/* endpoints backed by the
// OpenD protobuf protocol (TCP 11111): /v1/futu/account and /v1/futu/orders.
// Connection reuse, serialization and bounded reconnect are centralized in
// internal/futu, so every process user shares one connection per address.

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"

	"github.com/jiayu/wbot/internal/futu"
)

// protoClient retains only endpoint configuration; internal/futu owns the
// process-wide shared connection.
type protoClient struct {
	addr string
}

func newProtoClient() *protoClient {
	return newProtoClientAt(FutuAccountAddr())
}

// newProtoClientAt returns a protoClient dialing addr directly (tests).
func newProtoClientAt(addr string) *protoClient {
	return &protoClient{addr: addr}
}

// do leases the process-wide shared client. Close only returns the lease; the
// TCP connection stays open for other HTTP, wheel, CLI and Telegram users.
func (p *protoClient) do(ctx context.Context, fn func(*futu.TradeClient) error) error {
	tc, err := futu.AcquireTrade(ctx, p.addr)
	if err != nil {
		return err
	}
	defer tc.Close()
	return fn(tc)
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
