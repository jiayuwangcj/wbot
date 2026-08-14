package ingest

// HKEX end-of-day stock-option backfill.
//
// The official DTOP file supplies settlement price, turnover and gross open
// interest. RP006-FINAL supplies the same series' implied volatility and the
// underlying settlement price. option_quotes retains those official values.
// For the existing snapshot-backed Wheel research runner, the adapter also
// creates an explicitly source=hkex EOD projection: bid=ask=settlement and
// Black-Scholes delta/theta derived from the official IV with r=0. This is a
// deterministic research mark, not a historical executable order book.

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/wheelstore"
)

const (
	HKEXDataSource       = "hkex"
	HKEXAdjust           = "none"
	HKEXDefaultDTOPBase  = "https://www.hkex.com.hk/eng/stat/dmstat/oi"
	HKEXDefaultRP006Base = "https://www.hkex.com.hk/eng/market/rm/rm_dcrm/riskdata/srprices"
	HKEXMaxBackfillDays  = 730
	hkexMaxArchiveBytes  = 32 << 20
)

var (
	ErrHKEXNoFile    = errors.New("ingest: hkex source: no trading-day file")
	hkexRetryBackoff = []time.Duration{time.Second, 2 * time.Second}
	hkexLocation     = time.FixedZone("HKT", 8*60*60)
)

// HKEXInstrument identifies one stock-option class. The defaults used by the
// CLI are HK.00700/TCH/100; explicit values keep deterministic acceptance
// fixtures isolated without pretending there is a complete symbol master.
type HKEXInstrument struct {
	Underlying string
	Class      string
	LotSize    int64
}

func (i HKEXInstrument) Validate() error {
	if strings.TrimSpace(i.Underlying) == "" {
		return errors.New("ingest: hkex source: underlying is required")
	}
	class := strings.ToUpper(strings.TrimSpace(i.Class))
	if len(class) < 3 || len(class) > 6 {
		return errors.New("ingest: hkex source: option class must be 3..6 alphanumeric characters")
	}
	for _, r := range class {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return errors.New("ingest: hkex source: option class must be 3..6 alphanumeric characters")
		}
	}
	if i.LotSize <= 0 {
		return errors.New("ingest: hkex source: lot size must be positive")
	}
	return nil
}

// HKEXSource downloads one DTOP + RP006 pair at a time. Request starts pass a
// shared per-source limiter; the CLI constructs it with an interval >=1s for
// official hosts. Tests may use a shorter interval against loopback only.
type HKEXSource struct {
	Client          *http.Client
	DTOPBase        string
	RP006Base       string
	RequestInterval time.Duration
	limiter         *futu.Limiter
}

// HKEXDay is one atomic business-day payload ready for persistence. Snapshots
// is empty when HKEX published DTOP but no usable RP006 supplement.
type HKEXDay struct {
	BusinessDate time.Time
	ObservedAt   time.Time
	Quotes       []OptionQuoteRow
	Snapshots    []wheelstore.QuoteSnapshotRecord
}

// HKEXBackfillStats reports downloaded and inserted rows separately so a
// repeated idempotent run is visible as fetched rows with zero inserts.
type HKEXBackfillStats struct {
	TradingDays       int
	MissingDays       int
	QuoteOnlyDays     int
	QuoteRows         int
	SnapshotRows      int
	InsertedQuotes    int64
	InsertedSnapshots int64
}

type hkexDTOPSeries struct {
	Market     string
	Class      string
	Expiry     time.Time
	Strike     float64
	OptionType string
	Settlement float64
	Volume     int64
	GrossOI    int64
	RPSeries   string
}

type hkexRPSeries struct {
	Settlement float64
	IVPercent  float64
}

func (s *HKEXSource) requestLimiter() *futu.Limiter {
	if s.limiter == nil {
		gap := s.RequestInterval
		if gap <= 0 {
			gap = time.Second
		}
		s.limiter = futu.NewLimiter(gap)
	}
	return s.limiter
}

// FetchDay downloads and parses one official business date. A missing/empty
// DTOP is ErrHKEXNoFile (weekends should be filtered by the caller; holidays
// land here). When HKEX explicitly publishes an unavailable RP006 marker, the
// official DTOP settlement rows are retained without a research projection.
func (s *HKEXSource) FetchDay(ctx context.Context, date time.Time, instrument HKEXInstrument) (*HKEXDay, error) {
	if err := instrument.Validate(); err != nil {
		return nil, err
	}
	date = dateOnly(date)
	if date.IsZero() {
		return nil, errors.New("ingest: hkex source: business date is required")
	}
	dtopBase := strings.TrimRight(strings.TrimSpace(s.DTOPBase), "/")
	if dtopBase == "" {
		dtopBase = HKEXDefaultDTOPBase
	}
	rpBase := strings.TrimRight(strings.TrimSpace(s.RP006Base), "/")
	if rpBase == "" {
		rpBase = HKEXDefaultRP006Base
	}
	dtopURL := fmt.Sprintf("%s/DTOP_O_%s.zip", dtopBase, date.Format("20060102"))
	dtopZip, status, err := s.get(ctx, dtopURL)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrHKEXNoFile
	}
	noTrading, err := zipHasMember(dtopZip, "no_trading_activities.txt")
	if err != nil {
		return nil, fmt.Errorf("ingest: hkex source: DTOP: %w", err)
	}
	if noTrading {
		return nil, ErrHKEXNoFile
	}
	dtopRaw, err := zipMember(dtopZip, "_dtop_o_seoch_opt_dtl_all.raw")
	if err != nil {
		return nil, fmt.Errorf("ingest: hkex source: DTOP: %w", err)
	}
	parsed, err := parseDTOP(dtopRaw, date, strings.ToUpper(strings.TrimSpace(instrument.Class)))
	if err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		hasDetails, err := dtopHasDetailRows(dtopRaw)
		if err != nil {
			return nil, fmt.Errorf("ingest: hkex source: DTOP: %w", err)
		}
		if !hasDetails {
			return nil, ErrHKEXNoFile
		}
	}
	rpURL := fmt.Sprintf("%s/RP006_%s.zip", rpBase, date.Format("060102"))
	rpZip, status, err := s.get(ctx, rpURL)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return buildHKEXDay(date, instrument, parsed, nil, 0, false)
	}
	unavailable, err := zipHasMember(rpZip, "rp006.txt")
	if err != nil {
		return nil, fmt.Errorf("ingest: hkex source: RP006: %w", err)
	}
	if unavailable {
		note, err := zipMember(rpZip, "rp006.txt")
		if err != nil {
			return nil, fmt.Errorf("ingest: hkex source: RP006: %w", err)
		}
		if !strings.Contains(strings.ToLower(string(note)), "no file available") {
			return nil, fmt.Errorf("ingest: hkex source: RP006: unexpected rp006.txt marker %q", strings.TrimSpace(string(note)))
		}
		return buildHKEXDay(date, instrument, parsed, nil, 0, false)
	}
	return parseHKEXDay(date, instrument, dtopZip, rpZip)
}

func (s *HKEXSource) get(ctx context.Context, requestURL string) ([]byte, int, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt <= len(hkexRetryBackoff); attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(hkexRetryBackoff[attempt-1])
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, 0, ctx.Err()
			case <-timer.C:
			}
		}
		if err := s.requestLimiter().Wait(ctx); err != nil {
			return nil, 0, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("ingest: hkex source: request: %w", err)
		}
		// HKEX's CDN returns 406 to non-browser-shaped Accept/User-Agent pairs.
		// Keep wbot identifiable inside a compatible UA and accept the archive's
		// provider-selected MIME type (real files are still verified as ZIP).
		req.Header.Set("Accept", "*/*")
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; wbot-hkex-datafill/1.0)")
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, hkexMaxArchiveBytes+1))
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			continue
		}
		if closeErr != nil {
			lastErr = fmt.Errorf("close response: %w", closeErr)
			continue
		}
		if len(body) > hkexMaxArchiveBytes {
			return nil, resp.StatusCode, errors.New("ingest: hkex source: archive exceeds 32 MiB")
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, resp.StatusCode, nil
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, resp.StatusCode, nil
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			break
		}
	}
	return nil, 0, fmt.Errorf("ingest: hkex source: %w", lastErr)
}

func parseHKEXDay(date time.Time, instrument HKEXInstrument, dtopZip, rpZip []byte) (*HKEXDay, error) {
	class := strings.ToUpper(strings.TrimSpace(instrument.Class))
	dtopRaw, err := zipMember(dtopZip, "_dtop_o_seoch_opt_dtl_all.raw")
	if err != nil {
		return nil, fmt.Errorf("ingest: hkex source: DTOP: %w", err)
	}
	rpRaw, err := zipMember(rpZip, "_rp006-final_o.raw")
	if err != nil {
		// Some historical/test archives publish only the non-final SEOCH file.
		rpRaw, err = zipMember(rpZip, "_rp006_o.raw")
		if err != nil {
			return nil, fmt.Errorf("ingest: hkex source: RP006: %w", err)
		}
	}
	dtop, err := parseDTOP(dtopRaw, date, class)
	if err != nil {
		return nil, err
	}
	rp, underlyingPrice, err := parseRP006(rpRaw, date, class)
	if err != nil {
		return nil, err
	}
	return buildHKEXDay(date, instrument, dtop, rp, underlyingPrice, true)
}

func buildHKEXDay(date time.Time, instrument HKEXInstrument, dtop []hkexDTOPSeries, rp map[string]hkexRPSeries, underlyingPrice float64, requireProjection bool) (*HKEXDay, error) {
	class := strings.ToUpper(strings.TrimSpace(instrument.Class))
	observedAt := time.Date(date.Year(), date.Month(), date.Day(), 16, 0, 0, 0, hkexLocation).UTC()
	day := &HKEXDay{BusinessDate: dateOnly(date), ObservedAt: observedAt}
	snapshotKey := "hkex-eod-" + date.Format("20060102") + "-bs-r0"
	for _, series := range dtop {
		rpSeries, found := rp[series.RPSeries]
		if found && math.Abs(rpSeries.Settlement-series.Settlement) > 1e-6 {
			return nil, fmt.Errorf("ingest: hkex source: %s settlement mismatch DTOP=%g RP006=%g", series.RPSeries, series.Settlement, rpSeries.Settlement)
		}
		var iv *float64
		if found && rpSeries.IVPercent > 0 && finiteFloat(rpSeries.IVPercent) {
			decimal := rpSeries.IVPercent / 100
			iv = &decimal
		}
		symbol := hkexOptionSymbol(class, series.Expiry, series.OptionType, series.Strike)
		day.Quotes = append(day.Quotes, OptionQuoteRow{
			Symbol: symbol, Underlying: instrument.Underlying, OptionType: strings.ToLower(series.OptionType),
			Strike: series.Strike, Expiry: series.Expiry, Ts: observedAt,
			Open: series.Settlement, High: series.Settlement, Low: series.Settlement, Close: series.Settlement,
			Volume: series.Volume, ImpliedVol: iv,
		})
		if !found || iv == nil || series.Settlement <= 0 || series.Volume <= 0 || series.GrossOI <= 0 {
			continue
		}
		delta, theta, ok := blackScholesGreeks(underlyingPrice, series.Strike, *iv, observedAt, series.Expiry, series.OptionType)
		if !ok {
			continue
		}
		settlement := series.Settlement
		volume, oi, lot := series.Volume, series.GrossOI, instrument.LotSize
		spot := underlyingPrice
		day.Snapshots = append(day.Snapshots, wheelstore.QuoteSnapshotRecord{
			Symbol: symbol, Underlying: instrument.Underlying, OptionType: strings.ToUpper(series.OptionType),
			Strike: series.Strike, Expiry: series.Expiry, Source: HKEXDataSource, SnapshotKey: snapshotKey,
			UnderlyingPrice: &spot, Delta: &delta, Bid: &settlement, Ask: &settlement, IV: iv, Theta: &theta,
			Volume: &volume, OpenInterest: &oi, LotSize: &lot, ObservedAt: observedAt,
		})
	}
	if len(day.Quotes) == 0 {
		return nil, fmt.Errorf("ingest: hkex source: no %s option rows for %s", class, date.Format("2006-01-02"))
	}
	if requireProjection && len(day.Snapshots) == 0 {
		return nil, fmt.Errorf("ingest: hkex source: no complete %s EOD research rows for %s", class, date.Format("2006-01-02"))
	}
	return day, nil
}

func zipMember(data []byte, suffix string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("bad zip: %w", err)
	}
	for _, f := range r.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), strings.ToLower(suffix)) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(rc, hkexMaxArchiveBytes+1))
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(body) > hkexMaxArchiveBytes {
			return nil, errors.New("uncompressed member exceeds 32 MiB")
		}
		return body, nil
	}
	return nil, fmt.Errorf("member *%s not found", suffix)
}

func zipHasMember(data []byte, suffix string) (bool, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false, fmt.Errorf("bad zip: %w", err)
	}
	for _, f := range r.File {
		if strings.HasSuffix(strings.ToLower(f.Name), strings.ToLower(suffix)) {
			return true, nil
		}
	}
	return false, nil
}

func dtopHasDetailRows(data []byte) (bool, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if len(record) > 0 && record[0] == "01" {
			return true, nil
		}
	}
}

func parseDTOP(data []byte, expected time.Time, class string) ([]hkexDTOPSeries, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil || len(header) < 4 || header[0] != "H" || header[1] != "DTOP" {
		return nil, errors.New("ingest: hkex source: invalid DTOP header")
	}
	if header[3] != expected.Format("20060102") {
		return nil, fmt.Errorf("ingest: hkex source: DTOP business date %q does not match %s", header[3], expected.Format("20060102"))
	}
	var out []hkexDTOPSeries
	for rowNo := 2; ; rowNo++ {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ingest: hkex source: DTOP row %d: %w", rowNo, err)
		}
		if len(record) == 0 || record[0] == "T" {
			continue
		}
		if len(record) != 22 || record[0] != "01" || !strings.EqualFold(record[3], class) {
			continue
		}
		expiry, err := parseDTOPExpiry(record[4], record[5], record[6])
		if err != nil {
			return nil, fmt.Errorf("ingest: hkex source: DTOP row %d: %w", rowNo, err)
		}
		strike, err := positiveFloat(record[7], "strike")
		if err != nil {
			return nil, fmt.Errorf("ingest: hkex source: DTOP row %d: %w", rowNo, err)
		}
		for _, side := range []struct {
			kind                   string
			oi, volume, settlement int
		}{
			{kind: "CALL", oi: 8, volume: 11, settlement: 13},
			{kind: "PUT", oi: 15, volume: 18, settlement: 20},
		} {
			settlement, err := positiveFloat(record[side.settlement], "settlement")
			if err != nil {
				continue
			}
			volume, err := nonNegativeInt(record[side.volume], "turnover")
			if err != nil {
				return nil, fmt.Errorf("ingest: hkex source: DTOP row %d: %w", rowNo, err)
			}
			oi, err := nonNegativeInt(record[side.oi], "gross OI")
			if err != nil {
				return nil, fmt.Errorf("ingest: hkex source: DTOP row %d: %w", rowNo, err)
			}
			out = append(out, hkexDTOPSeries{
				Market: record[1], Class: class, Expiry: expiry, Strike: strike, OptionType: side.kind,
				Settlement: settlement, Volume: volume, GrossOI: oi,
				RPSeries: rpSeriesName(class, strike, expiry, side.kind, record[1]),
			})
		}
	}
	return out, nil
}

func parseRP006(data []byte, expected time.Time, class string) (map[string]hkexRPSeries, float64, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil || len(header) < 4 || header[0] != "H" || !strings.HasPrefix(header[1], "RP006") {
		return nil, 0, errors.New("ingest: hkex source: invalid RP006 header")
	}
	if header[3] != expected.Format("20060102") {
		return nil, 0, fmt.Errorf("ingest: hkex source: RP006 business date %q does not match %s", header[3], expected.Format("20060102"))
	}
	series := make(map[string]hkexRPSeries)
	var underlying float64
	for rowNo := 2; ; rowNo++ {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("ingest: hkex source: RP006 row %d: %w", rowNo, err)
		}
		if len(record) < 11 || record[0] != "01" || !strings.EqualFold(record[4], class) {
			continue
		}
		settlement, err := positiveFloat(record[7], "settlement")
		if err != nil {
			continue
		}
		name := strings.TrimSpace(record[1])
		if strings.EqualFold(name, class+"SP") {
			underlying = settlement
			continue
		}
		iv, err := strconv.ParseFloat(strings.TrimSpace(record[10]), 64)
		if err != nil || !finiteFloat(iv) || iv < 0 {
			iv = 0
		}
		series[name] = hkexRPSeries{Settlement: settlement, IVPercent: iv}
	}
	if underlying <= 0 {
		return nil, 0, fmt.Errorf("ingest: hkex source: RP006 missing %sSP underlying settlement", class)
	}
	return series, underlying, nil
}

func parseDTOPExpiry(day, month, year string) (time.Time, error) {
	monthNumber := map[string]time.Month{
		"JAN": time.January, "FEB": time.February, "MAR": time.March, "APR": time.April,
		"MAY": time.May, "JUN": time.June, "JUL": time.July, "AUG": time.August,
		"SEP": time.September, "OCT": time.October, "NOV": time.November, "DEC": time.December,
	}[strings.ToUpper(strings.TrimSpace(month))]
	d, dErr := strconv.Atoi(strings.TrimSpace(day))
	y, yErr := strconv.Atoi(strings.TrimSpace(year))
	if monthNumber == 0 || dErr != nil || yErr != nil || d < 1 || d > 31 || y < 0 || y > 99 {
		return time.Time{}, fmt.Errorf("invalid expiry %s-%s-%s", year, month, day)
	}
	expiry := time.Date(2000+y, monthNumber, d, 0, 0, 0, 0, time.UTC)
	if expiry.Day() != d || expiry.Month() != monthNumber {
		return time.Time{}, fmt.Errorf("invalid expiry %s-%s-%s", year, month, day)
	}
	return expiry, nil
}

func rpSeriesName(class string, strike float64, expiry time.Time, optionType, market string) string {
	callMonths := "ABCDEFGHIJKL"
	putMonths := "MNOPQRSTUVWX"
	codes := callMonths
	if strings.EqualFold(optionType, "PUT") {
		codes = putMonths
	}
	name := fmt.Sprintf("%s%.2f%c%d", class, strike, codes[int(expiry.Month())-1], expiry.Year()%10)
	if strings.EqualFold(strings.TrimSpace(market), "WSO") {
		name += fmt.Sprintf("W%02d", expiry.Day())
	}
	return name
}

func hkexOptionSymbol(class string, expiry time.Time, optionType string, strike float64) string {
	side := "C"
	if strings.EqualFold(optionType, "PUT") {
		side = "P"
	}
	return fmt.Sprintf("HK.%s%s%s%d", class, expiry.Format("060102"), side, int64(math.Round(strike*1000)))
}

func positiveFloat(raw, name string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || !finiteFloat(v) || v <= 0 {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	return v, nil
}

func nonNegativeInt(raw, name string) (int64, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	return v, nil
}

func finiteFloat(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func dateOnly(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	t = t.In(hkexLocation)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// blackScholesGreeks returns market delta and theta per calendar day. The
// official IV is decimal (0.20 = 20%); risk-free and dividend rates are fixed
// to zero to keep this research projection deterministic and auditable.
func blackScholesGreeks(spot, strike, iv float64, observedAt, expiryDate time.Time, optionType string) (float64, float64, bool) {
	expiryAt := time.Date(expiryDate.Year(), expiryDate.Month(), expiryDate.Day(), 16, 0, 0, 0, hkexLocation).UTC()
	tYears := expiryAt.Sub(observedAt).Hours() / (24 * 365)
	if spot <= 0 || strike <= 0 || iv <= 0 || tYears <= 0 || !finiteFloat(spot) || !finiteFloat(strike) || !finiteFloat(iv) {
		return 0, 0, false
	}
	sqrtT := math.Sqrt(tYears)
	d1 := (math.Log(spot/strike) + 0.5*iv*iv*tYears) / (iv * sqrtT)
	callDelta := 0.5 * (1 + math.Erf(d1/math.Sqrt2))
	phi := math.Exp(-0.5*d1*d1) / math.Sqrt(2*math.Pi)
	theta := -(spot * phi * iv / (2 * sqrtT)) / 365
	delta := callDelta
	if strings.EqualFold(optionType, "PUT") {
		delta = callDelta - 1
		if delta >= 0 {
			delta = -1e-12
		}
	} else if delta <= 0 {
		delta = 1e-12
	}
	if !finiteFloat(delta) || !finiteFloat(theta) {
		return 0, 0, false
	}
	return delta, theta, true
}

// InsertHKEXDay persists both official option_quotes and the EOD research
// projection in one transaction. Snapshot conflicts do nothing; quote
// conflicts may only fill a previously-null IV after an RP006 gap. Retry does
// not duplicate rows or overwrite an existing official value.
func InsertHKEXDay(ctx context.Context, db *sql.DB, day *HKEXDay) (quotes, snapshots int64, err error) {
	if db == nil {
		return 0, 0, errors.New("ingest: hkex: nil db")
	}
	if day == nil || day.BusinessDate.IsZero() || len(day.Quotes) == 0 {
		return 0, 0, errors.New("ingest: hkex: day has no official quotes")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	quoteStmt, err := tx.PrepareContext(ctx, `
INSERT INTO option_quotes
(symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'none','hkex')
ON CONFLICT (symbol, ts, adjust, source) DO UPDATE
SET implied_vol = COALESCE(option_quotes.implied_vol, EXCLUDED.implied_vol)
WHERE option_quotes.implied_vol IS NULL AND EXCLUDED.implied_vol IS NOT NULL`)
	if err != nil {
		return 0, 0, err
	}
	defer quoteStmt.Close()
	for _, q := range day.Quotes {
		res, err := quoteStmt.ExecContext(ctx, q.Symbol, q.Underlying, q.OptionType, q.Strike, q.Expiry, q.Ts,
			q.Open, q.High, q.Low, q.Close, q.Volume, q.ImpliedVol)
		if err != nil {
			return 0, 0, fmt.Errorf("ingest: hkex: insert option quote %s: %w", q.Symbol, err)
		}
		if n, e := res.RowsAffected(); e == nil {
			quotes += n
		}
	}
	if len(day.Snapshots) > 0 {
		snapshotStmt, err := tx.PrepareContext(ctx, `
INSERT INTO option_quote_snapshots
(symbol, underlying, option_type, strike, expiry, source, snapshot_key, underlying_price, delta, bid, ask, iv, theta, volume, open_interest, lot_size, observed_at)
VALUES ($1,$2,$3,$4,$5,'hkex',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (underlying, observed_at, snapshot_key, symbol) DO NOTHING`)
		if err != nil {
			return 0, 0, err
		}
		defer snapshotStmt.Close()
		for _, q := range day.Snapshots {
			res, err := snapshotStmt.ExecContext(ctx, q.Symbol, q.Underlying, q.OptionType, q.Strike, q.Expiry,
				q.SnapshotKey, q.UnderlyingPrice, q.Delta, q.Bid, q.Ask, q.IV, q.Theta,
				q.Volume, q.OpenInterest, q.LotSize, q.ObservedAt)
			if err != nil {
				return 0, 0, fmt.Errorf("ingest: hkex: insert snapshot %s: %w", q.Symbol, err)
			}
			if n, e := res.RowsAffected(); e == nil {
				snapshots += n
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return quotes, snapshots, nil
}

// BeginHKEXRun/FinishHKEXRun keep the long, incrementally committed backfill
// observable in ingestion_runs. Completed days remain durable if a later
// download fails; rerunning fills the remainder idempotently.
func BeginHKEXRun(ctx context.Context, db *sql.DB, source string) (int64, error) {
	if db == nil {
		return 0, errors.New("ingest: hkex: nil db")
	}
	if strings.TrimSpace(source) == "" {
		return 0, errors.New("ingest: hkex: empty run source")
	}
	var id int64
	if err := db.QueryRowContext(ctx, `INSERT INTO ingestion_runs (source,status) VALUES ($1,'running') RETURNING id`, source).Scan(&id); err != nil {
		return 0, fmt.Errorf("ingest: hkex: insert run: %w", err)
	}
	return id, nil
}

func FinishHKEXRun(ctx context.Context, db *sql.DB, id int64, succeeded bool) error {
	status := "failed"
	if succeeded {
		status = "succeeded"
	}
	res, err := db.ExecContext(ctx, `UPDATE ingestion_runs SET finished_at=now(),status=$2 WHERE id=$1`, id, status)
	if err != nil {
		return fmt.Errorf("ingest: hkex: finish run: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n != 1 {
		return fmt.Errorf("ingest: hkex: run %d not found", id)
	}
	return nil
}
