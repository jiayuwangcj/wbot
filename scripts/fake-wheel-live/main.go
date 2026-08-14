package main

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jiayu/wbot/internal/futu/fakegw"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
	"google.golang.org/protobuf/proto"
)

const (
	packetHeaderSize = 44
	protoInit        = 1001
	protoFunds       = 2101
	protoPositions   = 2102
)

type fakeState struct {
	mu      sync.Mutex
	dismiss string
	update  map[string]any
	sent    int
}

func main() {
	restListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err)
	}
	protoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err)
	}
	state := &fakeState{}
	server := &http.Server{Handler: http.HandlerFunc(state.handleHTTP)}
	go func() { _ = server.Serve(restListener) }()
	go serveProto(protoListener)
	fmt.Printf("FAKE_BASE_URL=http://%s\nPROTO_ADDR=%s\n\n", restListener.Addr(), protoListener.Addr())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = server.Close()
	_ = protoListener.Close()
}

func (s *fakeState) handleHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/"):
		s.handleFutu(w, r)
	case r.URL.Path == "/v1/chat/completions":
		writeJSON(w, map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{
				"content": `{"verdict":"APPROVE","reasons":["acceptance fake"],"notes":""}`,
			}}},
		})
	case strings.HasSuffix(r.URL.Path, "/sendMessage"):
		s.handleSendMessage(w, r)
	case strings.HasSuffix(r.URL.Path, "/getUpdates"):
		s.handleGetUpdates(w)
	case strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func (s *fakeState) handleFutu(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/subscribe":
		writeFutu(w, map[string]any{})
	case "/api/quote":
		var req struct {
			SecurityList []struct {
				Market int    `json:"market"`
				Code   string `json:"code"`
			} `json:"security_list"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		items := make([]map[string]any, 0, len(req.SecurityList))
		for _, security := range req.SecurityList {
			price := 10.0
			if !strings.Contains(security.Code, "C") && !strings.Contains(security.Code, "P") {
				price = 100.0
			}
			// update_time is a zone-less wall clock the wbot side parses in the
			// market's local zone (US → America/New_York, otherwise +08). Format
			// "now" in the same zone so the parsed instant is ~now regardless of
			// the server's clock (a fixed UTC+8 here would parse 8h in the
			// future on a UTC server and the candidate is rejected as stale).
			loc := time.Local
			if security.Market == 11 {
				if ny, err := time.LoadLocation("America/New_York"); err == nil {
					loc = ny
				}
			}
			items = append(items, map[string]any{
				"security":    map[string]any{"market": security.Market, "code": security.Code},
				"cur_price":   price,
				"volume":      1000,
				"update_time": time.Now().Add(-30 * time.Second).In(loc).Format("2006-01-02 15:04:05"),
			})
		}
		writeFutu(w, map[string]any{"basic_qot_list": items})
	case "/api/option-quote":
		// single-leg combo quote: mid stands in for the (absent) order book
		var req struct {
			MultiLegs []struct {
				Security struct {
					Code string `json:"code"`
				} `json:"security"`
			} `json:"multi_legs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		delta := 0.5
		if len(req.MultiLegs) > 0 && strings.Contains(req.MultiLegs[0].Security.Code, "P") {
			delta = -0.5
		}
		writeFutu(w, map[string]any{"option_quote_list": []any{map[string]any{
			"price":         10.0,
			"mid":           10.05,
			"iv":            30.0,
			"delta":         delta,
			"theta":         -0.1,
			"open_interest": 1000,
			"contract_size": 100,
		}}})
	case "/api/option-chain":
		now := time.Now().UTC().AddDate(0, 0, 7)
		date := now.Format("2006-01-02")
		codeDate := now.Format("060102")
		// Underlying quotes at 100, so OTM strikes are put 90 / call 110.
		// The 100-parity pair stays for breadth; the wheel OTM hard mask
		// (strike < underlying for puts, > underlying for calls) rejects it.
		writeFutu(w, map[string]any{"option_chain": []any{map[string]any{
			"strike_time": date,
			"option": []any{
				map[string]any{
					"call": map[string]any{
						"basic":          map[string]any{"security": map[string]any{"market": 11, "code": "FAKE" + codeDate + "C100000"}, "lot_size": 100},
						"option_ex_data": map[string]any{"strike_price": 100.0},
					},
					"put": map[string]any{
						"basic":          map[string]any{"security": map[string]any{"market": 11, "code": "FAKE" + codeDate + "P100000"}, "lot_size": 100},
						"option_ex_data": map[string]any{"strike_price": 100.0},
					},
				},
				map[string]any{
					"call": map[string]any{
						"basic":          map[string]any{"security": map[string]any{"market": 11, "code": "FAKE" + codeDate + "C110000"}, "lot_size": 100},
						"option_ex_data": map[string]any{"strike_price": 110.0},
					},
					"put": map[string]any{
						"basic":          map[string]any{"security": map[string]any{"market": 11, "code": "FAKE" + codeDate + "P90000"}, "lot_size": 100},
						"option_ex_data": map[string]any{"strike_price": 90.0},
					},
				},
			},
		}}})
	default:
		http.NotFound(w, r)
	}
}

func (s *fakeState) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReplyMarkup struct {
			InlineKeyboard [][]struct {
				CallbackData string `json:"callback_data"`
			} `json:"inline_keyboard"`
		} `json:"reply_markup"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.mu.Lock()
	s.sent++
	if s.update == nil && len(req.ReplyMarkup.InlineKeyboard) > 0 && len(req.ReplyMarkup.InlineKeyboard[0]) >= 3 {
		s.dismiss = req.ReplyMarkup.InlineKeyboard[0][2].CallbackData
		s.update = map[string]any{
			"update_id": 1,
			"callback_query": map[string]any{
				"id":      "accept-callback",
				"from":    map[string]any{"id": 1},
				"data":    s.dismiss,
				"message": map[string]any{"chat": map[string]any{"id": 1}},
			},
		}
	}
	sent := s.sent
	s.mu.Unlock()
	fmt.Printf("fake-telegram: sendMessage=%d\n", sent)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *fakeState) handleGetUpdates(w http.ResponseWriter) {
	s.mu.Lock()
	update := s.update
	s.update = nil
	s.mu.Unlock()
	result := []any{}
	if update != nil {
		result = append(result, update)
	}
	writeJSON(w, map[string]any{"ok": true, "result": result})
}

func serveProto(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go serveProtoConn(conn)
	}
}

func serveProtoConn(conn net.Conn) {
	defer conn.Close()
	header := make([]byte, packetHeaderSize)
	for {
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		protoID := int32(binary.LittleEndian.Uint32(header[2:6]))
		bodyLen := int(binary.LittleEndian.Uint32(header[12:16]))
		if bodyLen < 0 || bodyLen > 1<<20 {
			return
		}
		body := make([]byte, bodyLen)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		response := protoResponse(protoID)
		out := make([]byte, packetHeaderSize+len(response))
		copy(out, "FT")
		binary.LittleEndian.PutUint32(out[2:6], uint32(protoID))
		copy(out[8:12], header[8:12])
		binary.LittleEndian.PutUint32(out[12:16], uint32(len(response)))
		sum := sha1.Sum(response)
		copy(out[16:36], sum[:])
		copy(out[packetHeaderSize:], response)
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

func protoResponse(protoID int32) []byte {
	if protoID == protoInit {
		return fakegw.InitBody(42)
	}
	if protoID == protoFunds {
		return fakegw.FundsBody(0, 1, fakeFunds())
	}
	if protoID == protoPositions {
		return fakegw.PositionsBody(0, 1, []*trdcommon.Position{fakePosition()})
	}
	return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1, 2)})
}

func fakeFunds() *trdcommon.Funds {
	// The wbot side fails unmarshal when any required proto field is absent
	// ("required field Trd_Common.Funds.xxx not set"); set all seven.
	power, totalAssets, cash, marketVal := 100000.0, 100000.0, 100000.0, 0.0
	frozen, debt, avl := 0.0, 0.0, 0.0
	return &trdcommon.Funds{
		Power:             &power,
		TotalAssets:       &totalAssets,
		Cash:              &cash,
		MarketVal:         &marketVal,
		FrozenCash:        &frozen,
		DebtCash:          &debt,
		AvlWithdrawalCash: &avl,
	}
}

func fakePosition() *trdcommon.Position {
	id := uint64(1)
	side := int32(0)
	code := "FAKE"
	name := "fake underlying"
	qty := 1000.0
	price := 100.0
	value := 100000.0
	market := int32(2)
	return &trdcommon.Position{
		PositionID:   &id,
		PositionSide: &side,
		Code:         &code,
		Name:         &name,
		Qty:          &qty,
		CanSellQty:   &qty,
		Price:        &price,
		Val:          &value,
		PlVal:        proto.Float64(0),
		SecMarket:    &market,
	}
}

func writeFutu(w http.ResponseWriter, s2c any) {
	writeJSON(w, map[string]any{"ret_type": 0, "ret_msg": nil, "err_code": nil, "s2c": s2c})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fake-wheel-live:", err)
	os.Exit(1)
}
