package gofutuapi

import (
	"fmt"
	"time"

	"github.com/qtopie/gofutuapi/gen/common"
	qotcommon "github.com/qtopie/gofutuapi/gen/qot/common"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetacclist"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetfunds"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetmarginratio"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetmaxtrdqtys"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetorderfee"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetorderfilllist"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetorderlist"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetpositionlist"
	"github.com/qtopie/gofutuapi/gen/trade/trdmodifyorder"
	"github.com/qtopie/gofutuapi/gen/trade/trdplaceorder"
	"github.com/qtopie/gofutuapi/gen/trade/trdunlocktrade"
	"google.golang.org/protobuf/proto"
)

var pendingOrderStatuses = []int32{
	int32(trdcommon.OrderStatus_OrderStatus_WaitingSubmit),
	int32(trdcommon.OrderStatus_OrderStatus_Submitting),
	int32(trdcommon.OrderStatus_OrderStatus_Submitted),
	int32(trdcommon.OrderStatus_OrderStatus_Filled_Part),
	int32(trdcommon.OrderStatus_OrderStatus_Cancelling_Part),
	int32(trdcommon.OrderStatus_OrderStatus_Cancelling_All),
	int32(trdcommon.OrderStatus_OrderStatus_TimeOut),
}

func PendingOrderStatuses() []int32 {
	statuses := make([]int32, len(pendingOrderStatuses))
	copy(statuses, pendingOrderStatuses)
	return statuses
}

func OrderStatusLabel(status int32) string {
	if name, ok := trdcommon.OrderStatus_name[status]; ok {
		return name
	}
	return fmt.Sprintf("%d", status)
}

// --- Accounts ---

func (c *FutuClient) GetTradeAccounts() ([]*trdcommon.TrdAcc, error) {
	return c.getTradeAccounts(
		int32(trdcommon.TrdCategory_TrdCategory_Security),
		true,
	)
}

func (c *FutuClient) FindTradeAccount(trdEnv trdcommon.TrdEnv, trdMarket trdcommon.TrdMarket) (*trdcommon.TrdAcc, error) {
	accounts, err := c.GetTradeAccounts()
	if err != nil {
		return nil, err
	}

	acc := findTradeAccount(accounts, int32(trdEnv), int32(trdMarket))
	if acc == nil {
		return nil, fmt.Errorf("trading account not found for env=%s market=%s", trdEnv.String(), trdMarket.String())
	}

	return acc, nil
}

func (c *FutuClient) getTradeAccounts(trdCategory int32, needGeneralSecAccount bool) ([]*trdcommon.TrdAcc, error) {
	userID := uint64(0)
	req := trdgetacclist.Request{
		C2S: &trdgetacclist.C2S{
			UserID:                &userID,
			TrdCategory:           &trdCategory,
			NeedGeneralSecAccount: &needGeneralSecAccount,
		},
	}
	sn := c.Conn.SendProto(TRD_GETACCLIST, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get acc list failed: %w", err)
	}

	var resp trdgetacclist.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get acc list unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get acc list failed: %s", resp.GetRetMsg())
	}

	s2c := resp.GetS2C()
	if s2c == nil || len(s2c.GetAccList()) == 0 {
		return nil, nil
	}

	return s2c.GetAccList(), nil
}

// --- Basic Functions ---

func (c *FutuClient) UnlockTrade(password string, securityFirm trdcommon.SecurityFirm) error {
	if c == nil || c.Conn == nil {
		return fmt.Errorf("futu client connection is nil")
	}
	if password == "" {
		return fmt.Errorf("trade password is empty")
	}

	unlock := true
	pwdMD5 := md5Hex(password)
	firm := int32(securityFirm)
	req := trdunlocktrade.Request{
		C2S: &trdunlocktrade.C2S{
			Unlock:       &unlock,
			PwdMD5:       &pwdMD5,
			SecurityFirm: &firm,
		},
	}
	sn := c.Conn.SendProto(TRD_UNLOCKTRADE, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return fmt.Errorf("unlock trade failed: %w", err)
	}

	var resp trdunlocktrade.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return fmt.Errorf("unlock trade unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return fmt.Errorf("unlock trade failed: %s", resp.GetRetMsg())
	}

	return nil
}

// --- Assets and Positions ---

func (c *FutuClient) GetFundsForAccount(acc *trdcommon.TrdAcc, refreshCache bool) (*trdcommon.Funds, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	header := c.tradeHeaderForAccount(acc, trdcommon.TrdMarket_TrdMarket_Unknown)
	currency := int32(trdcommon.Currency_Currency_USD)

	req := trdgetfunds.Request{
		C2S: &trdgetfunds.C2S{
			Header:       header,
			RefreshCache: &refreshCache,
			Currency:     &currency,
		},
	}
	sn := c.Conn.SendProto(TRD_GETFUNDS, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get funds failed: %w", err)
	}

	var resp trdgetfunds.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get funds unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get funds failed: %s", resp.GetRetMsg())
	}
	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetFunds(), nil
}

func (c *FutuClient) GetPositionsForAccount(acc *trdcommon.TrdAcc, refreshCache bool) ([]*trdcommon.Position, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	header := c.tradeHeaderForAccount(acc, trdcommon.TrdMarket_TrdMarket_Unknown)

	req := trdgetpositionlist.Request{
		C2S: &trdgetpositionlist.C2S{
			Header:       header,
			RefreshCache: &refreshCache,
		},
	}
	sn := c.Conn.SendProto(TRD_GETPOSITIONLIST, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get position list failed: %w", err)
	}

	var resp trdgetpositionlist.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get position list unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get position list failed: %s", resp.GetRetMsg())
	}
	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetPositionList(), nil
}

// --- Orders ---

func (c *FutuClient) GetPendingOrders(trdEnv trdcommon.TrdEnv, trdMarket trdcommon.TrdMarket) ([]*trdcommon.Order, error) {
	return c.GetOrderList(trdEnv, trdMarket, PendingOrderStatuses(), true)
}

func (c *FutuClient) GetOrderList(trdEnv trdcommon.TrdEnv, trdMarket trdcommon.TrdMarket, filterStatusList []int32, refreshCache bool) ([]*trdcommon.Order, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	acc, err := c.FindTradeAccount(trdEnv, trdMarket)
	if err != nil {
		return nil, err
	}

	return c.GetOrderListForAccount(acc, filterStatusList, refreshCache)
}

func (c *FutuClient) GetOrderListForAccount(acc *trdcommon.TrdAcc, filterStatusList []int32, refreshCache bool) ([]*trdcommon.Order, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	header := c.tradeHeaderForAccount(acc, trdcommon.TrdMarket_TrdMarket_Unknown)

	req := trdgetorderlist.Request{
		C2S: &trdgetorderlist.C2S{
			Header:           header,
			FilterStatusList: append([]int32(nil), filterStatusList...),
			RefreshCache:     &refreshCache,
		},
	}
	sn := c.Conn.SendProto(TRD_GETORDERLIST, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get order list failed: %w", err)
	}

	var resp trdgetorderlist.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get order list unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get order list failed: %s", resp.GetRetMsg())
	}
	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetOrderList(), nil
}

func (c *FutuClient) GetOrderFillList(trdEnv trdcommon.TrdEnv, trdMarket trdcommon.TrdMarket, refreshCache bool) ([]*trdcommon.OrderFill, error) {
	acc, err := c.FindTradeAccount(trdEnv, trdMarket)
	if err != nil {
		return nil, err
	}
	return c.GetOrderFillListForAccount(acc, refreshCache)
}

func (c *FutuClient) GetOrderFillListForAccount(acc *trdcommon.TrdAcc, refreshCache bool) ([]*trdcommon.OrderFill, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	header := c.tradeHeaderForAccount(acc, trdcommon.TrdMarket_TrdMarket_Unknown)

	req := trdgetorderfilllist.Request{
		C2S: &trdgetorderfilllist.C2S{
			Header:       header,
			RefreshCache: &refreshCache,
		},
	}
	sn := c.Conn.SendProto(TRD_GETORDERFILLLIST, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get order fill list failed: %w", err)
	}

	var resp trdgetorderfilllist.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get order fill list unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get order fill list failed: %s", resp.GetRetMsg())
	}
	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetOrderFillList(), nil
}

func (c *FutuClient) ModifyOrder(acc *trdcommon.TrdAcc, orderID string, price float64, qty float64, op trdcommon.ModifyOrderOp) error {
	if c == nil || c.Conn == nil {
		return fmt.Errorf("futu client connection is nil")
	}

	header := c.tradeHeaderForAccount(acc, trdcommon.TrdMarket_TrdMarket_Unknown)

	req := trdmodifyorder.Request{
		C2S: &trdmodifyorder.C2S{
			PacketID:      c.GeneratePacketID(),
			Header:        header,
			OrderID:       nil, // Set below
			ModifyOrderOp: proto.Int32(int32(op)),
			Price:         &price,
			Qty:           &qty,
		},
	}

	var id uint64
	_, err := fmt.Sscanf(orderID, "%d", &id)
	if err == nil {
		req.C2S.OrderID = &id
	} else {
		return fmt.Errorf("invalid order ID format for Go SDK (numeric required): %s", orderID)
	}

	sn := c.Conn.SendProto(TRD_MODIFYORDER, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return fmt.Errorf("modify order failed: %w", err)
	}

	var resp trdmodifyorder.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return fmt.Errorf("modify order unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return fmt.Errorf("modify order failed: %s", resp.GetRetMsg())
	}

	return nil
}

func (c *FutuClient) PlaceOrder(acc *trdcommon.TrdAcc, trdSide trdcommon.TrdSide, orderType trdcommon.OrderType, code string, qty float64, price float64, secMarket qotcommon.QotMarket, trdMarket trdcommon.TrdMarket, timeInForce trdcommon.TimeInForce) (string, uint64, error) {
	if c == nil || c.Conn == nil {
		return "", 0, fmt.Errorf("futu client connection is nil")
	}

	header := c.tradeHeaderForAccount(acc, trdMarket)

	side := int32(trdSide)
	oType := int32(orderType)
	sm := int32(secMarket)
	tif := int32(timeInForce)
	req := trdplaceorder.Request{
		C2S: &trdplaceorder.C2S{
			PacketID:    c.GeneratePacketID(),
			Header:      header,
			TrdSide:     &side,
			OrderType:   &oType,
			Code:        &code,
			Qty:         &qty,
			Price:       &price,
			SecMarket:   &sm,
			TimeInForce: &tif,
		},
	}

	sn := c.Conn.SendProto(TRD_PLACEORDER, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return "", 0, fmt.Errorf("place order failed: %w", err)
	}

	var resp trdplaceorder.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return "", 0, fmt.Errorf("place order unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return "", 0, fmt.Errorf("place order failed: %s", resp.GetRetMsg())
	}

	s2c := resp.GetS2C()
	if s2c == nil {
		return "", 0, fmt.Errorf("place order response S2C is nil")
	}

	return s2c.GetOrderIDEx(), s2c.GetOrderID(), nil
}

// --- Helpers ---

func (c *FutuClient) tradeHeaderForAccount(acc *trdcommon.TrdAcc, trdMarket trdcommon.TrdMarket) *trdcommon.TrdHeader {
	trdEnv := acc.GetTrdEnv()
	accID := acc.GetAccID()

	// 如果传入了明确的市场，则使用该市场；否则尝试获取账户第一个授权市场
	tm := int32(trdMarket)
	if trdMarket == trdcommon.TrdMarket_TrdMarket_Unknown && len(acc.TrdMarketAuthList) > 0 {
		tm = acc.TrdMarketAuthList[0]
	}

	return &trdcommon.TrdHeader{
		TrdEnv:    &trdEnv,
		AccID:     &accID,
		TrdMarket: &tm,
	}
}

func findTradeAccount(accounts []*trdcommon.TrdAcc, trdEnv int32, trdMarket int32) *trdcommon.TrdAcc {
	for _, acc := range accounts {
		if acc.GetTrdEnv() != trdEnv {
			continue
		}
		for _, market := range acc.TrdMarketAuthList {
			if market == trdMarket {
				return acc
			}
		}
	}
	return nil
}

func (c *FutuClient) GeneratePacketID() *common.PacketID {
	connID := c.Conn.connId
	serialNo := uint32(c.Conn.nextPacketSN)
	return &common.PacketID{
		ConnID:   &connID,
		SerialNo: &serialNo,
	}
}

// --- Advanced Queries ---

func (c *FutuClient) GetOrderFee(acc *trdcommon.TrdAcc, orderIdExList []string) ([]*trdcommon.OrderFee, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := trdgetorderfee.Request{
		C2S: &trdgetorderfee.C2S{
			Header:        c.tradeHeaderForAccount(acc, trdcommon.TrdMarket_TrdMarket_Unknown),
			OrderIdExList: orderIdExList,
		},
	}
	sn := c.Conn.SendProto(TRD_GETORDERFEE, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get order fee failed: %w", err)
	}

	var resp trdgetorderfee.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get order fee unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get order fee failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetOrderFeeList(), nil
}

func (c *FutuClient) GetMaxTrdQtys(acc *trdcommon.TrdAcc, orderType trdcommon.OrderType, code string, price float64, secMarket qotcommon.QotMarket) (*trdcommon.MaxTrdQtys, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	ot := int32(orderType)
	sm := int32(secMarket)
	req := trdgetmaxtrdqtys.Request{
		C2S: &trdgetmaxtrdqtys.C2S{
			Header:    c.tradeHeaderForAccount(acc, trdcommon.TrdMarket_TrdMarket_Unknown),
			OrderType: &ot,
			Code:      &code,
			Price:     &price,
			SecMarket: &sm,
		},
	}
	sn := c.Conn.SendProto(TRD_GETMAXTRDQTYS, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get max trd qtys failed: %w", err)
	}

	var resp trdgetmaxtrdqtys.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get max trd qtys unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get max trd qtys failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetMaxTrdQtys(), nil
}

func (c *FutuClient) GetMarginRatio(acc *trdcommon.TrdAcc, securityList []*qotcommon.Security) ([]*trdgetmarginratio.MarginRatioInfo, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := trdgetmarginratio.Request{
		C2S: &trdgetmarginratio.C2S{
			Header:       c.tradeHeaderForAccount(acc, trdcommon.TrdMarket_TrdMarket_Unknown),
			SecurityList: securityList,
		},
	}
	sn := c.Conn.SendProto(TRD_GETMARGINRATIO, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get margin ratio failed: %w", err)
	}

	var resp trdgetmarginratio.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get margin ratio unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get margin ratio failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetMarginRatioInfoList(), nil
}
