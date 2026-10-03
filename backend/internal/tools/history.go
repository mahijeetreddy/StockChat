package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// rangeEnum is shared by history-style tool schemas.
const rangeEnum = `["1D","5D","1M","3M","6M","1Y","5Y"]`

// PriceChart is the price_chart UI payload.
type PriceChart struct {
	Symbol        string              `json:"symbol"`
	Range         domain.HistoryRange `json:"range"`
	Intraday      bool                `json:"intraday"`
	Candles       []domain.Candle     `json:"candles"`
	Start         float64             `json:"start"`
	End           float64             `json:"end"`
	Change        float64             `json:"change"`
	ChangePercent float64             `json:"change_percent"`
	High          float64             `json:"high"`
	Low           float64             `json:"low"`
}

// HistoryStats summarises a candle series.
type HistoryStats struct {
	Start, End, Change, ChangePercent float64
	High, Low                         float64
	HighAt, LowAt                     time.Time
	MaxVolume                         float64
	MaxVolumeAt                       time.Time
}

// Summarize computes summary statistics. candles must be non-empty and ascending.
func Summarize(candles []domain.Candle) HistoryStats {
	first, last := candles[0], candles[len(candles)-1]
	s := HistoryStats{Start: first.Open, End: last.Close, High: first.High, Low: first.Low, HighAt: first.Time, LowAt: first.Time}
	if s.Start == 0 {
		s.Start = first.Close
	}
	for _, c := range candles {
		if c.High > s.High {
			s.High, s.HighAt = c.High, c.Time
		}
		if c.Low < s.Low {
			s.Low, s.LowAt = c.Low, c.Time
		}
		if c.Volume > s.MaxVolume {
			s.MaxVolume, s.MaxVolumeAt = c.Volume, c.Time
		}
	}
	s.Change = s.End - s.Start
	if s.Start != 0 {
		s.ChangePercent = s.Change / s.Start * 100
	}
	return s
}

// Sample returns up to n evenly spaced candles, always including first and last.
func Sample(candles []domain.Candle, n int) []domain.Candle {
	if len(candles) <= n || n < 2 {
		return candles
	}
	out := make([]domain.Candle, 0, n)
	for i := range n {
		idx := i * (len(candles) - 1) / (n - 1)
		out = append(out, candles[idx])
	}
	return out
}

func timeLabel(t time.Time, intraday bool) string {
	t = t.In(market.NewYork)
	if intraday {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("2006-01-02")
}

type historyInput struct {
	Symbol string `json:"symbol"`
	Range  string `json:"range"`
}

func parseRange(s string) (domain.HistoryRange, error) {
	r := domain.HistoryRange(strings.ToUpper(strings.TrimSpace(s)))
	if r == "" {
		return domain.Range1M, nil
	}
	if !r.Valid() {
		return "", inputErr("range must be one of 1D, 5D, 1M, 3M, 6M, 1Y, 5Y")
	}
	return r, nil
}

// GetHistory is the get_history tool.
type GetHistory struct {
	Market market.Provider
}

// Spec implements Tool.
func (t *GetHistory) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "get_history",
		Description: "Get price history for one US ticker over a range and summary stats: start and end price, % change, high and low with dates, " +
			"and the highest-volume day. Use for 'how has X done this week/month/year', performance, trends, or charts. " +
			"Ranges: 1D (today intraday), 5D, 1M, 3M, 6M, 1Y, 5Y. The UI shows a chart automatically. " +
			"To compare several tickers, prefer compare_symbols.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"symbol":{"type":"string","description":"US ticker, e.g. TSLA"},` +
			`"range":{"type":"string","enum":` + rangeEnum + `,"description":"Lookback window; defaults to 1M"}` +
			`},"required":["symbol","range"],"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *GetHistory) Mutating() bool { return false }

// Label implements Labeler.
func (t *GetHistory) Label(input json.RawMessage) string {
	var in historyInput
	_ = json.Unmarshal(input, &in)
	sym := strings.ToUpper(strings.TrimSpace(in.Symbol))
	if !symbolRe.MatchString(sym) {
		return "Loading price history…"
	}
	if r := domain.HistoryRange(strings.ToUpper(in.Range)); r.Valid() {
		return fmt.Sprintf("Loading %s %s chart…", sym, r)
	}
	return fmt.Sprintf("Loading %s chart…", sym)
}

// Execute implements Tool.
func (t *GetHistory) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var in historyInput
	if err := decodeStrict(input, &in); err != nil {
		return Result{}, err
	}
	sym, err := NormalizeSymbol(in.Symbol)
	if err != nil {
		return Result{}, err
	}
	r, err := parseRange(in.Range)
	if err != nil {
		return Result{}, err
	}
	candles, err := t.Market.History(ctx, sym, r)
	if err != nil {
		return Result{}, fmt.Errorf("get_history %s %s: %w", sym, r, err)
	}
	if len(candles) == 0 {
		return Result{}, fmt.Errorf("get_history %s %s: no candles: %w", sym, r, market.ErrNotFound)
	}
	st := Summarize(candles)
	intra := r.Intraday()

	type point struct {
		T     string  `json:"t"`
		Close float64 `json:"close"`
	}
	sampled := Sample(candles, 10)
	points := make([]point, len(sampled))
	for i, c := range sampled {
		points[i] = point{T: timeLabel(c.Time, intra), Close: round(c.Close, 2)}
	}
	fm, err := marshalForModel(map[string]any{
		"symbol":         sym,
		"range":          r,
		"bars":           len(candles),
		"from":           timeLabel(candles[0].Time, intra),
		"to":             timeLabel(candles[len(candles)-1].Time, intra),
		"start":          round(st.Start, 2),
		"end":            round(st.End, 2),
		"change":         round(st.Change, 2),
		"change_percent": round(st.ChangePercent, 2),
		"high":           round(st.High, 2),
		"high_at":        timeLabel(st.HighAt, intra),
		"low":            round(st.Low, 2),
		"low_at":         timeLabel(st.LowAt, intra),
		"highest_volume": map[string]any{"at": timeLabel(st.MaxVolumeAt, intra), "volume": st.MaxVolume},
		"sampled_closes": points,
	})
	if err != nil {
		return Result{}, err
	}
	ui, err := NewUIBlock(UIPriceChart, PriceChart{
		Symbol: sym, Range: r, Intraday: intra, Candles: candles,
		Start: round(st.Start, 2), End: round(st.End, 2), Change: round(st.Change, 2),
		ChangePercent: round(st.ChangePercent, 2), High: round(st.High, 2), Low: round(st.Low, 2),
	})
	if err != nil {
		return Result{}, err
	}
	return Result{ForModel: fm, UI: ui}, nil
}
