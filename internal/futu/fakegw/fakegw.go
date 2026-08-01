// Package fakegw implements a minimal futu OpenD-protocol (TCP 11111) fake
// gateway for tests: FT-framed packets answered with canned protobuf bodies.
// Wire framing mirrors qtopie/gofutuapi protocol.go; only tests import this
// package, so it is never linked into the wbot binary.
package fakegw

import (
	"crypto/sha1"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/qtopie/gofutuapi/gen/common/initconnect"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetacclist"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetfunds"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetpositionlist"
	"github.com/qtopie/gofutuapi/gen/trade/trdplaceorder"
	"google.golang.org/protobuf/proto"
)

// headerSize is the OpenD packet header: "FT" + protoID + fmt + ver + serialNo
// + bodyLen + bodySHA1 + reserved.
const headerSize = 2 + 4 + 1 + 1 + 4 + 4 + 20 + 8

// Handler returns the response body for a request packet; nil closes the conn.
type Handler func(protoID int32, reqBody []byte) []byte

// Server starts a fake gateway on a loopback port and returns its address.
func Server(t *testing.T, h Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(conn, h)
		}
	}()
	return ln.Addr().String()
}

func serve(conn net.Conn, h Handler) {
	defer conn.Close()
	head := make([]byte, headerSize)
	for {
		if _, err := io.ReadFull(conn, head); err != nil {
			return
		}
		protoID := int32(binary.LittleEndian.Uint32(head[2:6]))
		bodyLen := int32(binary.LittleEndian.Uint32(head[12:16]))
		body := make([]byte, bodyLen)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		resp := h(protoID, body)
		if resp == nil {
			return
		}
		hdr := make([]byte, headerSize)
		copy(hdr, "FT")
		binary.LittleEndian.PutUint32(hdr[2:6], uint32(protoID))
		copy(hdr[8:12], head[8:12]) // echo the request serialNo (replies match on it)
		binary.LittleEndian.PutUint32(hdr[12:16], uint32(len(resp)))
		sum := sha1.Sum(resp)
		copy(hdr[16:36], sum[:])
		if _, err := conn.Write(append(hdr, resp...)); err != nil {
			return
		}
	}
}

// InitBody is the initconnect.Response body every connection expects first.
// All S2C fields are proto2-required, so defaults are set explicitly.
func InitBody(connID uint64) []byte {
	s2c := &initconnect.S2C{
		ServerVer:         proto.Int32(1002),
		LoginUserID:       proto.Uint64(0),
		ConnID:            proto.Uint64(connID),
		ConnAESKey:        proto.String(""),
		KeepAliveInterval: proto.Int32(0),
	}
	b, _ := proto.Marshal(&initconnect.Response{RetType: proto.Int32(0), S2C: s2c})
	return b
}

// Acc builds a TrdAcc for canned account lists (env: 0=simulate, 1=real).
func Acc(env int32, accID uint64, markets ...int32) *trdcommon.TrdAcc {
	return &trdcommon.TrdAcc{
		TrdEnv:            proto.Int32(env),
		AccID:             proto.Uint64(accID),
		TrdMarketAuthList: markets,
	}
}

// AccountsBody is a trdgetacclist response with ret_type 0.
func AccountsBody(accs []*trdcommon.TrdAcc) []byte {
	b, _ := proto.Marshal(&trdgetacclist.Response{RetType: proto.Int32(0), S2C: &trdgetacclist.S2C{AccList: accs}})
	return b
}

// Header builds a TrdHeader (proto2-required fields) for response S2Cs.
func Header(env int32, accID uint64, market int32) *trdcommon.TrdHeader {
	return &trdcommon.TrdHeader{
		TrdEnv:    proto.Int32(env),
		AccID:     proto.Uint64(accID),
		TrdMarket: proto.Int32(market),
	}
}

// FundsBody is a trdgetfunds response with ret_type 0.
func FundsBody(env int32, accID uint64, funds *trdcommon.Funds) []byte {
	b, _ := proto.Marshal(&trdgetfunds.Response{RetType: proto.Int32(0), S2C: &trdgetfunds.S2C{
		Header: Header(env, accID, 1),
		Funds:  funds,
	}})
	return b
}

// PositionsBody is a trdgetpositionlist response with ret_type 0.
func PositionsBody(env int32, accID uint64, pos []*trdcommon.Position) []byte {
	b, _ := proto.Marshal(&trdgetpositionlist.Response{RetType: proto.Int32(0), S2C: &trdgetpositionlist.S2C{
		Header:       Header(env, accID, 1),
		PositionList: pos,
	}})
	return b
}

// PlaceOrderBody is a trdplaceorder response with ret_type 0.
func PlaceOrderBody(env int32, accID uint64, orderIDEx string, orderID uint64) []byte {
	b, _ := proto.Marshal(&trdplaceorder.Response{RetType: proto.Int32(0), S2C: &trdplaceorder.S2C{
		Header:    Header(env, accID, 1),
		OrderIDEx: proto.String(orderIDEx),
		OrderID:   proto.Uint64(orderID),
	}})
	return b
}
