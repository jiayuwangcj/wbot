package gofutuapi

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/qtopie/gofutuapi/gen/common/getuserinfo"
	"github.com/qtopie/gofutuapi/gen/qot/qotupdatebasicqot"
	"github.com/qtopie/gofutuapi/gen/qot/qotupdatebroker"
	"github.com/qtopie/gofutuapi/gen/qot/qotupdatekl"
	"github.com/qtopie/gofutuapi/gen/qot/qotupdateorderbook"
	"github.com/qtopie/gofutuapi/gen/qot/qotupdatepricereminder"
	"github.com/qtopie/gofutuapi/gen/qot/qotupdatert"
	"github.com/qtopie/gofutuapi/gen/qot/qotupdateticker"
	"github.com/qtopie/gofutuapi/gen/trade/trdupdateorder"
	"github.com/qtopie/gofutuapi/gen/trade/trdupdateorderfill"
	"google.golang.org/protobuf/proto"
)

type FutuClient struct {
	Conn         *FutuApiConn
	pushHandlers map[ProtoId]func(proto.Message)
}

func NewClient(conn *FutuApiConn) *FutuClient {
	c := &FutuClient{
		Conn:         conn,
		pushHandlers: make(map[ProtoId]func(proto.Message)),
	}
	// Register the hook into connection
	conn.RegisterHook(c.dispatchPush)
	return c
}

func (c *FutuClient) dispatchPush(protoId ProtoId, resp *ProtoResponse) {
	handler, ok := c.pushHandlers[protoId]
	if !ok {
		return
	}

	msg := c.createPushMessage(protoId)
	if msg == nil {
		return
	}

	if err := proto.Unmarshal(resp.Payload, msg); err != nil {
		fmt.Printf("push unmarshal error for proto %d: %v\n", protoId, err)
		return
	}

	handler(msg)
}

func (c *FutuClient) OnPush(protoId ProtoId, handler func(proto.Message)) {
	c.pushHandlers[protoId] = handler
}

func (c *FutuClient) createPushMessage(protoId ProtoId) proto.Message {
	switch protoId {
	case TRD_UPDATEORDER:
		return &trdupdateorder.Response{}
	case TRD_UPDATEORDERFILL:
		return &trdupdateorderfill.Response{}
	case QOT_UPDATEBASICQOT:
		return &qotupdatebasicqot.Response{}
	case QOT_UPDATEKL:
		return &qotupdatekl.Response{}
	case QOT_UPDATERT:
		return &qotupdatert.Response{}
	case QOT_UPDATETICKER:
		return &qotupdateticker.Response{}
	case QOT_UPDATEORDERBOOK:
		return &qotupdateorderbook.Response{}
	case QOT_UPDATEBROKER:
		return &qotupdatebroker.Response{}
	case QOT_UPDATEPRICEREMINDER:
		return &qotupdatepricereminder.Response{}
	}
	return nil
}

// --- Basic Functions ---

func (c *FutuClient) GetUserInfo() (*getuserinfo.S2C, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	flag := int32(getuserinfo.UserInfoField_UserInfoField_Basic)
	req := getuserinfo.Request{
		C2S: &getuserinfo.C2S{
			Flag: &flag,
		},
	}
	sn := c.Conn.SendProto(GET_USER_INFO, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get user info failed: %w", err)
	}

	var resp getuserinfo.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get user info unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get user info failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C(), nil
}

// --- Shared Internal Helpers ---

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}
