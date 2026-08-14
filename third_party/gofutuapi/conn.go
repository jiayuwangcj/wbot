package gofutuapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qtopie/gofutuapi/gen/common/initconnect"
	"google.golang.org/protobuf/proto"
)

const defaultConnectTimeout = 10 * time.Second

var (
	clientID            = "gofutuapi"
	clientVer           = int32(0)
	recvNotify          = true
	packetEncAlgo       = int32(-1)
	pushProtoFmt        = int32(0)
	programmingLanguage = "Go"
)

type FutuApiOption struct {
	Address string
	Timeout time.Duration
}

type FutuApiConn struct {
	// connection-lifetime context (independent from the dial caller context)
	context.Context
	cancel context.CancelFunc

	option FutuApiOption

	// network connections
	net.Conn

	// server push packet on receive hook
	pushHook func(protoId ProtoId, response *ProtoResponse)
	// pending replies map
	pendingReplies map[int32]chan *ProtoResponse
	repliesMu      sync.Mutex

	connId       uint64
	nextPacketSN int32

	mu             sync.Mutex
	rw             io.ReadWriteCloser
	disconnected   chan struct{}
	disconnectOnce sync.Once
	disconnectMu   sync.Mutex
	disconnectErr  error
}

func Open(ctx context.Context, option FutuApiOption) (*FutuApiConn, error) {
	// Using the context from the parameter
	return openWithRetry(ctx, option)
}

func OpenContext(ctx context.Context, option FutuApiOption) (*FutuApiConn, error) {
	return openWithRetry(ctx, option)
}

// openWithRetry is the internal constructor
func openWithRetry(ctx context.Context, option FutuApiOption) (*FutuApiConn, error) {
	lifetimeCtx, cancel := context.WithCancel(context.Background())
	c := &FutuApiConn{
		Context:        lifetimeCtx,
		cancel:         cancel,
		option:         option,
		pendingReplies: make(map[int32]chan *ProtoResponse),
		disconnected:   make(chan struct{}),
	}

	if err := c.connect(ctx); err != nil {
		cancel()
		return nil, err
	}

	// read on server response
	go c.handleResponsePacket()

	return c, nil
}

func (conn *FutuApiConn) connect(ctx context.Context) error {
	timeout := conn.option.Timeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	nc, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", conn.option.Address)
	if err != nil {
		return err
	}

	conn.Conn = nc
	conn.rw = conn.Conn
	atomic.StoreInt32(&conn.nextPacketSN, 1)

	// In connect, we handle initConnect manually to get connId before starting
	// handleResponsePacket. The handshake shares the configured dial timeout;
	// every failure closes the half-open socket before returning.
	if err := conn.initConnectSync(timeout); err != nil {
		nc.Close()
		conn.Conn = nil
		conn.rw = nil
		return err
	}
	return nil
}

func (conn *FutuApiConn) initConnectSync(timeout time.Duration) error {
	req := &initconnect.Request{
		C2S: &initconnect.C2S{
			ClientVer:           &clientVer,
			ClientID:            &clientID,
			RecvNotify:          &recvNotify,
			PacketEncAlgo:       &packetEncAlgo,
			PushProtoFmt:        &pushProtoFmt,
			ProgrammingLanguage: &programmingLanguage,
		},
	}

	packetSN := atomic.AddInt32(&conn.nextPacketSN, 1) - 1
	header := NewHeader()
	header.ProtoID = INIT_CONNECT
	header.SerialNo = packetSN
	payload, _ := proto.Marshal(req)
	header.UpdateBodyInfo(payload)
	data := append(header.ToBytes(), payload...)

	if _, err := conn.rw.Write(data); err != nil {
		return err
	}

	// Read reply immediately
	if err := conn.Conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	defer conn.Conn.SetReadDeadline(time.Time{})
	respBuf := make([]byte, HEADER_SIZE)
	if _, err := io.ReadFull(conn.rw, respBuf); err != nil {
		return err
	}
	respHeader := ParseHeader(respBuf)
	respPayload := make([]byte, respHeader.BodyLen)
	if _, err := io.ReadFull(conn.rw, respPayload); err != nil {
		return err
	}

	var resp initconnect.Response
	if err := proto.Unmarshal(respPayload, &resp); err != nil {
		return err
	}

	if resp.GetRetType() != 0 {
		return fmt.Errorf("init connect failed: %s", resp.GetRetMsg())
	}

	conn.connId = resp.GetS2C().GetConnID()
	log.Printf("inited connection with ID %d", conn.connId)

	return nil
}

func (conn *FutuApiConn) markDisconnected(err error) {
	conn.disconnectOnce.Do(func() {
		conn.disconnectMu.Lock()
		conn.disconnectErr = err
		conn.disconnectMu.Unlock()
		close(conn.disconnected)
	})
}

func (conn *FutuApiConn) disconnectedError() error {
	conn.disconnectMu.Lock()
	defer conn.disconnectMu.Unlock()
	if conn.disconnectErr != nil {
		return conn.disconnectErr
	}
	return errors.New("connection closed")
}

func (conn *FutuApiConn) SendProto(protoId ProtoId, req proto.Message) int32 {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	packetSN := atomic.AddInt32(&conn.nextPacketSN, 1) - 1

	// Register pending reply
	replyCh := make(chan *ProtoResponse, 1)
	conn.repliesMu.Lock()
	conn.pendingReplies[packetSN] = replyCh
	conn.repliesMu.Unlock()

	header := NewHeader()
	header.ProtoID = protoId
	header.SerialNo = packetSN

	payload, err := proto.Marshal(req)
	if err != nil {
		panic(err)
	}

	header.UpdateBodyInfo(payload)
	data := append(header.ToBytes(), payload...)
	_, err = conn.rw.Write(data)
	if err != nil {
		log.Printf("write error for proto %d: %v", protoId, err)
		conn.markDisconnected(err)
	}

	return packetSN
}

func (conn *FutuApiConn) RegisterHook(f func(protoId ProtoId, response *ProtoResponse)) {
	conn.pushHook = f
}

func (conn *FutuApiConn) GetConnID() uint64 {
	return conn.connId
}

func (conn *FutuApiConn) Close() error {
	conn.cancel()
	conn.markDisconnected(net.ErrClosed)
	if conn.Conn != nil {
		log.Println("closing connection", conn.connId)
		return conn.Conn.Close()
	}
	return nil
}

func (conn *FutuApiConn) WaitReply(sn int32, timeout time.Duration) (*ProtoResponse, error) {
	conn.repliesMu.Lock()
	ch, ok := conn.pendingReplies[sn]
	conn.repliesMu.Unlock()

	if !ok {
		return nil, fmt.Errorf("no pending reply for SN %d", sn)
	}

	defer func() {
		conn.repliesMu.Lock()
		delete(conn.pendingReplies, sn)
		conn.repliesMu.Unlock()
	}()

	select {
	case d := <-ch:
		return d, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for reply SN %d", sn)
	case <-conn.disconnected:
		return nil, fmt.Errorf("connection closed: %w", conn.disconnectedError())
	case <-conn.Done():
		return nil, errors.New("connection closed")
	}
}

func (conn *FutuApiConn) handleResponsePacket() {
	for {
		select {
		case <-conn.Done():
			return
		default:
			buffer := make([]byte, HEADER_SIZE)
			_, err := io.ReadFull(conn.rw, buffer)
			if err != nil {
				select {
				case <-conn.Done():
					return
				default:
					log.Printf("read header error: %v", err)
					conn.markDisconnected(err)
					return
				}
			}

			h := ParseHeader(buffer[:])
			payload := make([]byte, h.BodyLen)
			_, err = io.ReadFull(conn.rw, payload)
			if err != nil {
				log.Printf("read payload error: %v", err)
				conn.markDisconnected(err)
				return
			}

			resp := &ProtoResponse{
				Header:  *h,
				Payload: payload,
			}

			// check if push or reply
			if h.SerialNo == 0 || IsPushProto(h.ProtoID) {
				if conn.pushHook != nil {
					conn.pushHook(h.ProtoID, resp)
				}
			} else {
				conn.repliesMu.Lock()
				ch, ok := conn.pendingReplies[h.SerialNo]
				if ok {
					select {
					case ch <- resp:
					default:
						log.Printf("reply channel full for SN %d", h.SerialNo)
					}
				} else {
					// Some replies might not be waited for, but we should log if it's unexpected
					// log.Printf("unexpected reply packet SN %d ProtoID %d", h.SerialNo, h.ProtoID)
				}
				conn.repliesMu.Unlock()
			}
		}
	}
}
