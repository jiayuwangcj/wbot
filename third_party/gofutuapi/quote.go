package gofutuapi

import (
	"fmt"
	"time"

	"github.com/qtopie/gofutuapi/gen/qot/common"
	"github.com/qtopie/gofutuapi/gen/qot/getoptionexpirationdate"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetbroker"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetcapitaldistribution"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetcapitalflow"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetkl"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetmarketstate"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetoptionchain"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetorderbook"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetownerplate"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetplatesecurity"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetplateset"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetrt"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetsecuritysnapshot"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetticker"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetusersecurity"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetusersecuritygroup"
	"github.com/qtopie/gofutuapi/gen/qot/qotgetwarrant"
	"github.com/qtopie/gofutuapi/gen/qot/qotmodifyusersecurity"
	"github.com/qtopie/gofutuapi/gen/qot/qotrequesthistorykl"
	"github.com/qtopie/gofutuapi/gen/qot/qotrequestrehab"
	"github.com/qtopie/gofutuapi/gen/qot/qotrequesttradedate"
	"github.com/qtopie/gofutuapi/gen/qot/qotsetpricereminder"
	"github.com/qtopie/gofutuapi/gen/qot/qotstockfilter"
	"github.com/qtopie/gofutuapi/gen/qot/qotsub"
	"google.golang.org/protobuf/proto"
)

// --- Real-Time Data ---

func (c *FutuClient) Subscribe(securityList []*common.Security, subTypeList []int32, isSub bool, isRegPush bool) error {
	if c == nil || c.Conn == nil {
		return fmt.Errorf("futu client connection is nil")
	}

	req := qotsub.Request{
		C2S: &qotsub.C2S{
			SecurityList:     securityList,
			SubTypeList:      subTypeList,
			IsSubOrUnSub:     &isSub,
			IsRegOrUnRegPush: &isRegPush,
		},
	}
	sn := c.Conn.SendProto(QOT_SUB, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	var resp qotsub.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return fmt.Errorf("subscribe unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return fmt.Errorf("subscribe failed: %s", resp.GetRetMsg())
	}

	return nil
}

// --- Basic Data ---

func (c *FutuClient) GetKLine(security *common.Security, klType common.KLType, rehabType common.RehabType, count int32) ([]*common.KLine, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	k := int32(klType)
	r := int32(rehabType)
	req := qotgetkl.Request{
		C2S: &qotgetkl.C2S{
			Security:  security,
			KlType:    &k,
			RehabType: &r,
			ReqNum:    &count,
		},
	}
	sn := c.Conn.SendProto(QOT_GETKL, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get kline failed: %w", err)
	}

	var resp qotgetkl.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get kline unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get kline failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetKlList(), nil
}

func (c *FutuClient) GetTicker(security *common.Security, maxNum int32) ([]*common.Ticker, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetticker.Request{
		C2S: &qotgetticker.C2S{
			Security:  security,
			MaxRetNum: &maxNum,
		},
	}
	sn := c.Conn.SendProto(QOT_GETTICKER, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get ticker failed: %w", err)
	}

	var resp qotgetticker.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get ticker unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get ticker failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetTickerList(), nil
}

func (c *FutuClient) GetOrderBook(security *common.Security, num int32) ([]*common.OrderBook, []*common.OrderBook, error) {
	if c == nil || c.Conn == nil {
		return nil, nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetorderbook.Request{
		C2S: &qotgetorderbook.C2S{
			Security: security,
			Num:      &num,
		},
	}
	sn := c.Conn.SendProto(QOT_GETORDERBOOK, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("get order book failed: %w", err)
	}

	var resp qotgetorderbook.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, nil, fmt.Errorf("get order book unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, nil, fmt.Errorf("get order book failed: %s", resp.GetRetMsg())
	}

	s2c := resp.GetS2C()
	return s2c.GetOrderBookAskList(), s2c.GetOrderBookBidList(), nil
}

func (c *FutuClient) GetRTData(security *common.Security) ([]*common.TimeShare, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetrt.Request{
		C2S: &qotgetrt.C2S{
			Security: security,
		},
	}
	sn := c.Conn.SendProto(QOT_GETRT, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get rt data failed: %w", err)
	}

	var resp qotgetrt.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get rt data unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get rt data failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetRtList(), nil
}

func (c *FutuClient) GetSecuritySnapshot(securityList []*common.Security) ([]*qotgetsecuritysnapshot.Snapshot, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetsecuritysnapshot.Request{
		C2S: &qotgetsecuritysnapshot.C2S{
			SecurityList: securityList,
		},
	}
	sn := c.Conn.SendProto(QOT_GETSECURITYSNAPSHOT, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get security snapshot failed: %w", err)
	}

	var resp qotgetsecuritysnapshot.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get security snapshot unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get security snapshot failed: %s", resp.GetRetMsg())
	}

	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetSnapshotList(), nil
}

func (c *FutuClient) SetPriceReminder(security *common.Security, op qotsetpricereminder.SetPriceReminderOp, reminderType common.PriceReminderType, freq common.PriceReminderFreq, value float64, note string) (int64, error) {
	if c == nil || c.Conn == nil {
		return 0, fmt.Errorf("futu client connection is nil")
	}

	o := int32(op)
	t := int32(reminderType)
	f := int32(freq)
	req := qotsetpricereminder.Request{
		C2S: &qotsetpricereminder.C2S{
			Security: security,
			Op:       &o,
			Type:     &t,
			Freq:     &f,
			Value:    &value,
			Note:     &note,
		},
	}
	sn := c.Conn.SendProto(QOT_SETPRICEREMINDER, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return 0, fmt.Errorf("set price reminder failed: %w", err)
	}

	var resp qotsetpricereminder.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return 0, fmt.Errorf("set price reminder unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return 0, fmt.Errorf("set price reminder failed: %s", resp.GetRetMsg())
	}

	if resp.GetS2C() == nil {
		return 0, nil
	}

	return resp.GetS2C().GetKey(), nil
}

// --- Market Filter ---

func (c *FutuClient) StockFilter(market common.QotMarket, filters *qotstockfilter.C2S) (*qotstockfilter.S2C, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	m := int32(market)
	filters.Market = &m
	if filters.Begin == nil {
		filters.Begin = proto.Int32(0)
	}
	if filters.Num == nil {
		filters.Num = proto.Int32(50)
	}

	req := qotstockfilter.Request{
		C2S: filters,
	}
	sn := c.Conn.SendProto(QOT_STOCKFILTER, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("stock filter failed: %w", err)
	}

	var resp qotstockfilter.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("stock filter unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("stock filter failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C(), nil
}

// --- History Data ---

func (c *FutuClient) RequestHistoryKL(security *common.Security, klType common.KLType, rehabType common.RehabType, begin, end string, maxNum int32) ([]*common.KLine, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	var allKL []*common.KLine
	var nextKey []byte

	for {
		k := int32(klType)
		r := int32(rehabType)
		req := qotrequesthistorykl.Request{
			C2S: &qotrequesthistorykl.C2S{
				Security:   security,
				KlType:     &k,
				RehabType:  &r,
				BeginTime:  &begin,
				EndTime:    &end,
				NextReqKey: nextKey,
			},
		}
		if maxNum > 0 {
			req.C2S.MaxAckKLNum = &maxNum
		}

		sn := c.Conn.SendProto(QOT_REQUESTHISTORYKL, &req)

		reply, err := c.Conn.WaitReply(sn, 10*time.Second)
		if err != nil {
			return nil, fmt.Errorf("request history kl failed: %w", err)
		}

		var resp qotrequesthistorykl.Response
		if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
			return nil, fmt.Errorf("request history kl unmarshal failed: %w", err)
		}
		if resp.GetRetType() != 0 {
			return nil, fmt.Errorf("request history kl failed: %s", resp.GetRetMsg())
		}

		s2c := resp.GetS2C()
		if s2c == nil {
			break
		}
		allKL = append(allKL, s2c.GetKlList()...)
		nextKey = s2c.GetNextReqKey()

		if len(nextKey) == 0 {
			break
		}

		if maxNum > 0 && int32(len(allKL)) >= maxNum {
			break
		}
	}

	return allKL, nil
}

func (c *FutuClient) RequestRehab(security *common.Security) ([]*common.Rehab, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotrequestrehab.Request{
		C2S: &qotrequestrehab.C2S{
			Security: security,
		},
	}
	sn := c.Conn.SendProto(QOT_REQUESTREHAB, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("request rehab failed: %w", err)
	}

	var resp qotrequestrehab.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("request rehab unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("request rehab failed: %s", resp.GetRetMsg())
	}

	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetRehabList(), nil
}

// --- Related Derivatives ---

func (c *FutuClient) GetOptionExpirationDate(owner *common.Security) ([]*getoptionexpirationdate.OptionExpirationDate, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := getoptionexpirationdate.Request{
		C2S: &getoptionexpirationdate.C2S{
			Owner: owner,
		},
	}
	sn := c.Conn.SendProto(QOT_GETOPTIONEXPIRATIONDATE, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get option expiration date failed: %w", err)
	}

	var resp getoptionexpirationdate.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get option expiration date unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get option expiration date failed: %s", resp.GetRetMsg())
	}

	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetDateList(), nil
}

func (c *FutuClient) GetOptionChain(owner *common.Security, beginTime, endTime string, optionType common.OptionType) ([]*qotgetoptionchain.OptionChain, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	t := int32(optionType)
	req := qotgetoptionchain.Request{
		C2S: &qotgetoptionchain.C2S{
			Owner:     owner,
			BeginTime: &beginTime,
			EndTime:   &endTime,
			Type:      &t,
		},
	}
	sn := c.Conn.SendProto(QOT_GETOPTIONCHAIN, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get option chain failed: %w", err)
	}

	var resp qotgetoptionchain.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get option chain unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get option chain failed: %s", resp.GetRetMsg())
	}

	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetOptionChain(), nil
}

// --- User Security ---

func (c *FutuClient) GetUserSecurityGroup(groupType qotgetusersecuritygroup.GroupType) ([]*qotgetusersecuritygroup.GroupData, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	g := int32(groupType)
	req := qotgetusersecuritygroup.Request{
		C2S: &qotgetusersecuritygroup.C2S{
			GroupType: &g,
		},
	}
	sn := c.Conn.SendProto(QOT_GETUSERSECURITYGROUP, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get user security group failed: %w", err)
	}

	var resp qotgetusersecuritygroup.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get user security group unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get user security group failed: %s", resp.GetRetMsg())
	}

	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetGroupList(), nil
}

func (c *FutuClient) GetUserSecurity(groupName string) ([]*common.SecurityStaticInfo, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetusersecurity.Request{
		C2S: &qotgetusersecurity.C2S{
			GroupName: &groupName,
		},
	}
	sn := c.Conn.SendProto(QOT_GETUSERSECURITY, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get user security failed: %w", err)
	}

	var resp qotgetusersecurity.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get user security unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get user security failed: %s", resp.GetRetMsg())
	}

	if resp.GetS2C() == nil {
		return nil, nil
	}

	return resp.GetS2C().GetStaticInfoList(), nil
}

func (c *FutuClient) ModifyUserSecurity(groupName string, op qotmodifyusersecurity.ModifyUserSecurityOp, securityList []*common.Security) error {
	if c == nil || c.Conn == nil {
		return fmt.Errorf("futu client connection is nil")
	}

	o := int32(op)
	req := qotmodifyusersecurity.Request{
		C2S: &qotmodifyusersecurity.C2S{
			GroupName:    &groupName,
			Op:           &o,
			SecurityList: securityList,
		},
	}
	sn := c.Conn.SendProto(QOT_MODIFYUSERSECURITY, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return fmt.Errorf("modify user security failed: %w", err)
	}

	var resp qotmodifyusersecurity.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return fmt.Errorf("modify user security unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return fmt.Errorf("modify user security failed: %s", resp.GetRetMsg())
	}

	return nil
}

// --- Market Context ---

func (c *FutuClient) GetMarketState(securityList []*common.Security) ([]*qotgetmarketstate.MarketInfo, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetmarketstate.Request{
		C2S: &qotgetmarketstate.C2S{
			SecurityList: securityList,
		},
	}
	sn := c.Conn.SendProto(QOT_GETMARKETSTATE, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get market state failed: %w", err)
	}

	var resp qotgetmarketstate.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get market state unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get market state failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetMarketInfoList(), nil
}

func (c *FutuClient) RequestTradeDate(market common.QotMarket, beginTime, endTime string) ([]*qotrequesttradedate.TradeDate, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	m := int32(market)
	req := qotrequesttradedate.Request{
		C2S: &qotrequesttradedate.C2S{
			Market:    &m,
			BeginTime: &beginTime,
			EndTime:   &endTime,
		},
	}
	sn := c.Conn.SendProto(QOT_REQUESTTRADEDATE, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("request trade date failed: %w", err)
	}

	var resp qotrequesttradedate.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("request trade date unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("request trade date failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetTradeDateList(), nil
}

func (c *FutuClient) GetPlateList(market common.QotMarket, plateClass common.PlateSetType) ([]*common.PlateInfo, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	m := int32(market)
	p := int32(plateClass)
	req := qotgetplateset.Request{
		C2S: &qotgetplateset.C2S{
			Market:       &m,
			PlateSetType: &p,
		},
	}
	sn := c.Conn.SendProto(QOT_GETPLATESET, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get plate set failed: %w", err)
	}

	var resp qotgetplateset.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get plate set unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get plate set failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetPlateInfoList(), nil
}

func (c *FutuClient) GetPlateSecurity(plate *common.Security, sortField common.SortField, ascending bool) ([]*common.SecurityStaticInfo, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	s := int32(sortField)
	req := qotgetplatesecurity.Request{
		C2S: &qotgetplatesecurity.C2S{
			Plate:     plate,
			SortField: &s,
			Ascend:    &ascending,
		},
	}
	sn := c.Conn.SendProto(QOT_GETPLATESECURITY, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get plate security failed: %w", err)
	}

	var resp qotgetplatesecurity.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get plate security unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get plate security failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetStaticInfoList(), nil
}

func (c *FutuClient) GetOwnerPlate(securityList []*common.Security) ([]*qotgetownerplate.SecurityOwnerPlate, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetownerplate.Request{
		C2S: &qotgetownerplate.C2S{
			SecurityList: securityList,
		},
	}
	sn := c.Conn.SendProto(QOT_GETOWNERPLATE, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get owner plate failed: %w", err)
	}

	var resp qotgetownerplate.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get owner plate unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get owner plate failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetOwnerPlateList(), nil
}

// --- Specialized Data ---

func (c *FutuClient) GetCapitalFlow(security *common.Security) ([]*qotgetcapitalflow.CapitalFlowItem, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetcapitalflow.Request{
		C2S: &qotgetcapitalflow.C2S{
			Security: security,
		},
	}
	sn := c.Conn.SendProto(QOT_GETCAPITALFLOW, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get capital flow failed: %w", err)
	}

	var resp qotgetcapitalflow.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get capital flow unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get capital flow failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C().GetFlowItemList(), nil
}

func (c *FutuClient) GetCapitalDistribution(security *common.Security) (*qotgetcapitaldistribution.S2C, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetcapitaldistribution.Request{
		C2S: &qotgetcapitaldistribution.C2S{
			Security: security,
		},
	}
	sn := c.Conn.SendProto(QOT_GETCAPITALDISTRIBUTION, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get capital distribution failed: %w", err)
	}

	var resp qotgetcapitaldistribution.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("get capital distribution unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("get capital distribution failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C(), nil
}

func (c *FutuClient) StockFilterWarrant(filters *qotgetwarrant.C2S) (*qotgetwarrant.S2C, error) {
	if c == nil || c.Conn == nil {
		return nil, fmt.Errorf("futu client connection is nil")
	}

	if filters.Begin == nil {
		filters.Begin = proto.Int32(0)
	}
	if filters.Num == nil {
		filters.Num = proto.Int32(50)
	}

	req := qotgetwarrant.Request{
		C2S: filters,
	}
	sn := c.Conn.SendProto(QOT_GETWARRANT, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("stock filter warrant failed: %w", err)
	}

	var resp qotgetwarrant.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, fmt.Errorf("stock filter warrant unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, fmt.Errorf("stock filter warrant failed: %s", resp.GetRetMsg())
	}

	return resp.GetS2C(), nil
}

func (c *FutuClient) GetBrokerQueue(security *common.Security) ([]*common.Broker, []*common.Broker, error) {
	if c == nil || c.Conn == nil {
		return nil, nil, fmt.Errorf("futu client connection is nil")
	}

	req := qotgetbroker.Request{
		C2S: &qotgetbroker.C2S{
			Security: security,
		},
	}
	sn := c.Conn.SendProto(QOT_GETBROKER, &req)

	reply, err := c.Conn.WaitReply(sn, 10*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("get broker queue failed: %w", err)
	}

	var resp qotgetbroker.Response
	if err := proto.Unmarshal(reply.Payload, &resp); err != nil {
		return nil, nil, fmt.Errorf("get broker queue unmarshal failed: %w", err)
	}
	if resp.GetRetType() != 0 {
		return nil, nil, fmt.Errorf("get broker queue failed: %s", resp.GetRetMsg())
	}

	s2c := resp.GetS2C()
	return s2c.GetBrokerAskList(), s2c.GetBrokerBidList(), nil
}
