package futu

import (
	"context"
	"fmt"
	"strconv"

	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

// CancelOrder cancels a previously placed order (Trd_ModifyOrder with
// ModifyOrderOp_Cancel). orderID is the numeric order id returned by
// PlaceOrder; the Go SDK's ModifyOrder only accepts the numeric form.
// The caller (CLI / API) keeps the paper-first rule: cancelling in the real
// env still writes to the live account.
//
// The cancel is a trade write; it is rate-limited by QuoteLimit like
// PlaceOrder so burst cancels cannot trip the gateway's ban heuristics.
func (tc *TradeClient) CancelOrder(ctx context.Context, acc *trdcommon.TrdAcc, orderID string) error {
	if err := QuoteLimit.Wait(ctx); err != nil {
		return err
	}
	// The SDK's ModifyOrder re-parses the string as a numeric id; surface a
	// clearer error than its %d-sscanf failure for non-numeric input.
	if _, err := strconv.ParseUint(orderID, 10, 64); err != nil {
		return fmt.Errorf("bad order id %q (want the numeric order id from futu order)", orderID)
	}
	// price/qty are ignored by the gateway for a cancel op (HK supports it).
	const (
		ignoredPrice = 0.0
		ignoredQty   = 0.0
	)
	if err := tc.cli.ModifyOrder(acc, orderID, ignoredPrice, ignoredQty,
		trdcommon.ModifyOrderOp_ModifyOrderOp_Cancel); err != nil {
		return fmt.Errorf("cancel order %s: %w", orderID, err)
	}
	return nil
}
