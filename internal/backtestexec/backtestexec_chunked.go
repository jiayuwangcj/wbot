package backtestexec

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// RunChunked executes one DB-backed run by processing bars in consecutive time
// chunks, loading each chunk's bars just-in-time so the full window's bars are
// never resident at once. Portfolio, strategy and accumulator state carry
// across chunks. The option snapshot set is loaded once (it is small relative
// to bars and must rank IVs over the same window as the single-call run), so
// the Result and sourceHash are byte-identical to Run for the same window
// (doc/BACKTEST.md -chunk). The CLI exposes it via `wbot backtest -chunk 30d`.
func RunChunked(ctx context.Context, db *sql.DB, o Options, chunkSize time.Duration) (*Outcome, error) {
	if chunkSize <= 0 {
		return nil, errors.New("backtest: exec: chunked: chunk size must be > 0")
	}
	if err := validateInputs(o, db); err != nil {
		return nil, err
	}
	s, templ, err := Build(o.Strategy, o.Params)
	if err != nil {
		return nil, err
	}
	ApplyTrace(s, o)
	from, to, err := probeBarRange(ctx, db, o)
	if err != nil {
		return nil, err
	}
	feeModel := backtest.LegacyFeeModel(o.Fee)
	if o.FeeModel != nil {
		feeModel = *o.FeeModel
	}
	sess, err := backtest.NewSession(o.Cash, feeModel, o.Seed, s)
	if err != nil {
		return nil, err
	}
	hasher := newSourceHashStream()
	seenBatches := map[batchKey]struct{}{}

	// The full option snapshot set is loaded once, exactly like Prepare, so
	// quote batches and their IV ranks are byte-identical to the single-call
	// run; chunking only bounds the bars memory, which dominates for long 1m
	// windows. Per-chunk snapshot windows would rank IVs over truncated history
	// and break the determinism contract (doc/BACKTEST.md).
	var fullOpts *backtest.OptionsData
	if templ != nil && templ.NeedsOptions {
		singleQuoteFrom := quoteRangeStart(o.From, s)
		if !o.QuoteFrom.IsZero() {
			singleQuoteFrom = o.QuoteFrom
		}
		rows, err := wheelstore.New(db).QueryUnderlyingQuoteSnapshots(ctx, o.Symbol, singleQuoteFrom, to, o.Limit)
		if err != nil {
			return nil, err
		}
		fullOpts, err = optionsDataForRun(rows, o.Seed)
		if err != nil {
			return nil, err
		}
	}

	firstDataChunk := true
	var firstClose float64
	cur := from
	remaining := o.Limit
	for remaining > 0 {
		next, final := chunkEnd(cur, to, chunkSize)
		var bars []ingest.Bar
		var err error
		if final {
			bars, err = ingest.QueryBars(ctx, db, o.Symbol, o.Timeframe, o.Adjust, cur, next, remaining, false)
		} else {
			// Non-final chunks read [cur, next) so a bar exactly at the chunk
			// boundary belongs to the next chunk only; an inclusive upper bound
			// would double-process it across the two adjacent chunks and break
			// the single-run determinism contract (doc/BACKTEST.md).
			bars, err = ingest.QueryBarsExclusiveEnd(ctx, db, o.Symbol, o.Timeframe, o.Adjust, cur, next, remaining, false)
		}
		if err != nil {
			return nil, err
		}
		if len(bars) > 0 {
			ownedFrom := cur
			if firstDataChunk {
				// The first data chunk owns the pre-window snapshot region, the
				// same batches the single-call run's quoteRangeStart included.
				ownedFrom = quoteRangeStart(o.From, s)
				if !o.QuoteFrom.IsZero() {
					ownedFrom = o.QuoteFrom
				}
				firstClose = bars[0].Close
				firstDataChunk = false
			}
			remaining -= len(bars)
			hasher.addBars(bars)
			hasher.addOwnedBatches(fullOpts, ownedFrom, seenBatches)
			if err := sess.Process(ctx, bars, fullOpts, ownedFrom); err != nil {
				return nil, err
			}
		}
		cur = next
		if final {
			break
		}
	}
	if sess.BarCount() == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoBars, o.Symbol)
	}
	res, err := sess.Result(o.Cash, nil, nil)
	if err != nil {
		return nil, err
	}
	hashStr, err := hasher.digest()
	if err != nil {
		return nil, err
	}
	return &Outcome{
		Result:            res,
		StartTs:           sess.FirstBar(),
		EndTs:             sess.LastBar(),
		BaselineReturnPct: sess.Close()/firstClose - 1,
		SourceHash:        hashStr,
	}, nil
}

// chunkEnd returns the next chunk's upper bound and whether it is the final
// chunk. Non-final chunks read [cur, next) — the upper bound is exclusive so a
// bar exactly at next is not read until the following chunk (whose lower bound
// is inclusive), avoiding a double-process of the boundary bar. The final chunk
// reads [cur, to] closed, matching the single-call QueryBars window.
func chunkEnd(cur, to time.Time, size time.Duration) (next time.Time, final bool) {
	next = cur.Add(size)
	final = !next.Before(to) // next >= to → clamp to to, this is the last chunk
	if next.After(to) {
		next = to
	}
	return next, final
}

// probeBarRange fills zero From/To with the symbol's first/last bar so the
// chunk loop bounds match the single-call QueryBars window.
func probeBarRange(ctx context.Context, db *sql.DB, o Options) (from, to time.Time, err error) {
	from, to = o.From, o.To
	if from.IsZero() {
		first, err := ingest.QueryBars(ctx, db, o.Symbol, o.Timeframe, o.Adjust, time.Time{}, to, 1, false)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if len(first) == 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: %s", ErrNoBars, o.Symbol)
		}
		from = first[0].Ts
	}
	if to.IsZero() {
		last, err := ingest.QueryBars(ctx, db, o.Symbol, o.Timeframe, o.Adjust, from, time.Time{}, 1, true)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if len(last) == 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: %s", ErrNoBars, o.Symbol)
		}
		to = last[0].Ts
	}
	return from, to, nil
}

// sourceHashStream builds the same sha256 source fingerprint as sourceHash
// without holding every bar and quote batch at once. The byte stream mirrors
// json.Marshal of the {bars, quote_batches} snapshot struct, so the single-call
// and chunked paths produce one identical digest (determinism, doc/BACKTEST.md).
//
// Bars are streamed to the hash as they arrive (they are the memory hog); quote
// batches are buffered and emitted at digest() so the bars array always closes
// before the first quote_batch element, regardless of chunk interleaving.
type sourceHashStream struct {
	h        hash.Hash
	barCount int
	batches  []backtest.QuoteSnapshotBatch
	err      error
}

func newSourceHashStream() *sourceHashStream {
	h := sha256.New()
	h.Write([]byte(`{"bars":[`))
	return &sourceHashStream{h: h}
}

func (s *sourceHashStream) addBars(bars []ingest.Bar) {
	if s.err != nil {
		return
	}
	for _, b := range bars {
		if s.barCount > 0 {
			s.h.Write([]byte(","))
		}
		bj, err := json.Marshal(b)
		if err != nil {
			s.err = fmt.Errorf("backtest: hash bar: %w", err)
			return
		}
		s.h.Write(bj)
		s.barCount++
	}
}

// addOwnedBatches buffers the chunk's quote batches inside its owned range,
// skipping batches already buffered by an earlier chunk (overlapping lookback
// regions are deduplicated by batch identity, matching single-call sourceHash).
func (s *sourceHashStream) addOwnedBatches(opts *backtest.OptionsData, ownedFrom time.Time, seen map[batchKey]struct{}) {
	if s.err != nil || opts == nil {
		return
	}
	for _, batch := range opts.QuoteBatches {
		if !ownedFrom.IsZero() && batch.ObservedAt.Before(ownedFrom) {
			continue
		}
		key := batchKey{observed: batch.ObservedAt.UTC().UnixNano(), key: batch.SnapshotKey}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		s.batches = append(s.batches, batch)
	}
}

func (s *sourceHashStream) digest() (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if len(s.batches) > 0 {
		s.h.Write([]byte(`],"quote_batches":[`))
		for i, batch := range s.batches {
			if i > 0 {
				s.h.Write([]byte(","))
			}
			bj, err := json.Marshal(batch)
			if err != nil {
				return "", fmt.Errorf("backtest: hash quote batch: %w", err)
			}
			s.h.Write(bj)
		}
	}
	s.h.Write([]byte(`]}`))
	sum := s.h.Sum(nil)
	return fmt.Sprintf("sha256-%x", sum), nil
}

// batchKey identifies one quote batch for chunk-owned dedup; it must match the
// grouping key OptionsDataFromQuoteSnapshots uses (observed_at, snapshot_key).
type batchKey struct {
	observed int64
	key      string
}
