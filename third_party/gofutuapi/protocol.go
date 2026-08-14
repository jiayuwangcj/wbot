package gofutuapi

import (
	"crypto/sha1"
	"encoding/binary"
)

type ProtoId int32

const (
	HEADER_SIZE  = 2 + 4 + 1 + 1 + 4 + 4 + 20 + 8
	szHeaderFlag = "FT"

	// 初始化连接
	INIT_CONNECT ProtoId = 1001
	// 获取全局状态
	GETGLOBALSTATE ProtoId = 1002
	// 推送通知
	NOTIFY ProtoId = 1003
	// 心跳
	KEEP_ALIVE ProtoId = 1004

	GET_USER_INFO ProtoId = 1005

	// 订阅或者反订阅
	QOT_SUB ProtoId = 3001
	// 注册推送
	QOT_REGQOTPUSH ProtoId = 3002
	// 获取订阅信息
	QOT_GETSUBINFO ProtoId = 3003
	// 获取逐笔,调用该接口前需要先订阅(订阅位：Qot_Common.SubType_Ticker)
	QOT_GETTICKER ProtoId = 3010
	// 获取基本行情,调用该接口前需要先订阅(订阅位：Qot_Common.SubType_Basic)
	QOT_GETBASICQOT ProtoId = 3004
	// 获取摆盘,调用该接口前需要先订阅(订阅位：Qot_Common.SubType_OrderBook)
	QOT_GETORDERBOOK ProtoId = 3012
	// 获取K线，调用该接口前需要先订阅(订阅位：Qot_Common.SubType_KL_XXX)
	QOT_GETKL ProtoId = 3006
	// 获取分时，调用该接口前需要先订阅(订阅位：Qot_Common.SubType_RT)
	QOT_GETRT ProtoId = 3008
	// 获取经纪队列，调用该接口前需要先订阅(订阅位：Qot_Common.SubType_Broker)
	QOT_GETBROKER ProtoId = 3014
	// 获取本地历史复权信息
	QOT_GETREHAB ProtoId = 3102
	// 在线请求历史复权信息，不读本地历史数据DB
	QOT_REQUESTREHAB ProtoId = 3105
	// 在线请求历史K线，不读本地历史数据DB
	QOT_REQUESTHISTORYKL ProtoId = 3103
	// 获取历史K线已经用掉的额度
	QOT_REQUESTHISTORYKLQUOTA ProtoId = 3104
	// 获取交易日
	QOT_GETTRADEDATE ProtoId = 3200
	// 获取静态信息
	QOT_GETSTATICINFO ProtoId = 3202
	// 获取股票快照
	QOT_GETSECURITYSNAPSHOT ProtoId = 3203
	// 获取板块集合下的板块
	QOT_GETPLATESET ProtoId = 3204
	// 获取板块下的股票
	QOT_GETPLATESECURITY ProtoId = 3205
	// 获取相关股票
	QOT_GETREFERENCE ProtoId = 3206
	// 获取股票所属的板块
	QOT_GETOWNERPLATE ProtoId = 3207
	// 获取大股东持股变化列表
	QOT_GETHOLDINGCHANGELIST ProtoId = 3208
	// 筛选期权
	QOT_GETOPTIONCHAIN ProtoId = 3209
	// 筛选窝轮
	QOT_GETWARRANT ProtoId = 3210
	// 获取资金流向
	QOT_GETCAPITALFLOW ProtoId = 3211
	// 获取资金分布
	QOT_GETCAPITALDISTRIBUTION ProtoId = 3212
	// 获取自选股分组下的股票
	QOT_GETUSERSECURITY ProtoId = 3213
	// 修改自选股分组下的股票
	QOT_MODIFYUSERSECURITY ProtoId = 3214
	// 推送基本行情
	QOT_UPDATEBASICQOT ProtoId = 3005
	// 推送K线
	QOT_UPDATEKL ProtoId = 3007
	// 推送分时
	QOT_UPDATERT ProtoId = 3009
	// 推送逐笔
	QOT_UPDATETICKER ProtoId = 3011
	// 推送买卖盘
	QOT_UPDATEORDERBOOK ProtoId = 3013
	// 推送经纪队列
	QOT_UPDATEBROKER ProtoId = 3015
	// 到价提醒通知
	QOT_UPDATEPRICEREMINDER ProtoId = 3019
	// 获取条件选股
	QOT_STOCKFILTER ProtoId = 3215
	// 获取股票代码变化信息
	QOT_GETCODECHANGE ProtoId = 3216
	// 获取新股Ipo
	QOT_GETIPOLIST ProtoId = 3217
	// 获取期货合约资料
	QOT_GETFUTUREINFO ProtoId = 3218
	// 在线拉取交易日
	QOT_REQUESTTRADEDATE ProtoId = 3219
	// 设置到价提醒
	QOT_SETPRICEREMINDER ProtoId = 3220
	// 获取到价提醒
	QOT_GETPRICEREMINDER ProtoId = 3221
	// 获取自选股分组
	QOT_GETUSERSECURITYGROUP ProtoId = 3222
	// 获取指定品种的市场状态
	QOT_GETMARKETSTATE ProtoId = 3223
	// 获取期权到期日
	QOT_GETOPTIONEXPIRATIONDATE ProtoId = 3224

	// 获取交易账户列表
	TRD_GETACCLIST ProtoId = 2001
	// 解锁
	TRD_UNLOCKTRADE ProtoId = 2005
	// 订阅接收推送数据的交易账户
	TRD_SUBACCPUSH ProtoId = 2008
	// 获取账户资金
	TRD_GETFUNDS ProtoId = 2101
	// 获取账户持仓
	TRD_GETPOSITIONLIST ProtoId = 2102
	// 获取最大交易数量
	TRD_GETMAXTRDQTYS ProtoId = 2111
	// 获取当日订单列表
	TRD_GETORDERLIST ProtoId = 2201
	// 下单
	TRD_PLACEORDER ProtoId = 2202
	// 修改订单
	TRD_MODIFYORDER ProtoId = 2205
	// 订单状态变动通知(推送)
	TRD_UPDATEORDER ProtoId = 2208
	// 获取当日成交列表
	TRD_GETORDERFILLLIST ProtoId = 2211
	// 成交通知(推送)
	TRD_UPDATEORDERFILL ProtoId = 2218
	// 获取历史订单列表
	TRD_GETHISTORYORDERLIST ProtoId = 2221
	// 获取历史成交列表
	TRD_GETHISTORYORDERFILLLIST ProtoId = 2222
	// 获取融资融券数据
	TRD_GETMARGINRATIO ProtoId = 2223
	// 获取融资融券数据
	TRD_GETORDERFEE ProtoId = 2225
	// 获取资金流水
	TRD_GETFLOWSUMMARY ProtoId = 2226
)

// IsPushProto 检查给定的 protoID 是否为推送协议。
func IsPushProto(protoID ProtoId) bool {
	switch protoID {
	case QOT_UPDATEBASICQOT,
		QOT_UPDATEBROKER,
		QOT_UPDATEKL,
		QOT_UPDATEORDERBOOK,
		QOT_UPDATEPRICEREMINDER,
		QOT_UPDATERT,
		QOT_UPDATETICKER,
		TRD_UPDATEORDER,
		TRD_UPDATEORDERFILL,
		NOTIFY:
		return true
	default:
		return false
	}
}

type ProtoHeader struct {
	szHeaderFlag [2]byte
	ProtoID      ProtoId
	ProtoFmtType byte
	ProtoVer     byte
	SerialNo     int32
	BodyLen      int32
	arrBodySHA1  [20]byte
	arrReserved  [8]byte
}

func NewHeader() *ProtoHeader {
	header := &ProtoHeader{}

	header.arrReserved = [8]byte{}
	return header
}

func ParseHeader(data []byte) *ProtoHeader {
	if len(data) != HEADER_SIZE {
		panic("unmatched header size")
	}

	header := NewHeader()

	header.ProtoID = ProtoId(bytesToInt32(data[2:6]))
	header.ProtoFmtType = data[6]
	header.ProtoVer = data[7]
	header.SerialNo = bytesToInt32(data[8:12])
	header.BodyLen = bytesToInt32(data[12:16])
	copy(header.arrBodySHA1[:], data[16:36])
	copy(header.arrReserved[:], data[36:])
	return header
}

func (h *ProtoHeader) UpdateBodyInfo(b []byte) {
	h.BodyLen = int32(len(b))
	h.arrBodySHA1 = sha1.Sum(b)
}

func (h *ProtoHeader) ToBytes() []byte {
	data := make([]byte, HEADER_SIZE)
	copy(data, szHeaderFlag)
	copy(data[2:6], int32ToBytes(int32(h.ProtoID)))
	data[6] = h.ProtoFmtType
	data[7] = h.ProtoVer
	copy(data[8:12], int32ToBytes(h.SerialNo))
	copy(data[12:16], int32ToBytes(h.BodyLen))
	copy(data[16:36], h.arrBodySHA1[:])
	copy(data[36:], h.arrReserved[:])
	return data
}

func int32ToBytes(n int32) []byte {
	b := make([]byte, 4)

	binary.LittleEndian.PutUint32(b, uint32(n))

	return b
}

func bytesToInt32(b []byte) int32 {
	return int32(binary.LittleEndian.Uint32(b[:]))
}

type ProtoResponse struct {
	Header   ProtoHeader
	Payload  []byte
	SerialNo uint32
	RetType  int
}
