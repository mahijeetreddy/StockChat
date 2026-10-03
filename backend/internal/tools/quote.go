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

// marketStatus resolves whether the market is open, falling back to the
// regular-hours calendar if the provider call fails.
func marketStatus(ctx context.Context, p market.Provider, now time.Time) bool {
	open, err := p.MarketOpen(ctx)
	if err != nil {
		return market.IsRegularHours(now)
	}
	return open
}

// quoteForModel is the compact quote representation sent to the LLM.
type quoteForModel struct {
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price"`
	Change        float64 `json:"change"`
	ChangePercent float64 `json:"change_percent"`
	DayHigh       float64 `json:"day_high"`
	DayLow        float64 `json:"day_low"`
	PrevClose     float64 `json:"prev_close"`
	MarketOpen    bool    `json:"market_open"`
	AsOf          string  `json:"as_of"`
	Note          string  `json:"note,omitempty"`
}

func toQuoteForModel(q domain.Quote) quoteForModel {
	out := quoteForModel{
		Symbol:        q.Symbol,
		Price:         round(q.Price, 2),
		Change:        round(q.Change, 2),
		ChangePercent: round(q.ChangePercent, 2),
		DayHigh:       round(q.High, 2),
		DayLow:        round(q.Low, 2),
		PrevClose:     round(q.PrevClose, 2),
		MarketOpen:    q.MarketOpen,
		AsOf:          q.AsOf.In(market.NewYork).Format("2006-01-02 15:04 MST"),
	}
	if !q.MarketOpen {
		out.Note = "US market is closed; price is the last close/last trade as of as_of"
	}
	return out
}

// GetQuote is the get_quote tool.
type GetQuote struct {
	Market market.Provider
	Now    func() time.Time
}

type symbolInput struct {
	Symbol string `json:"symbol"`
}

// Spec implements Tool.
func (t *GetQuote) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "get_quote",
		Description: "Get the latest price for one US stock ticker: price, change vs previous close, % change, day high/low, " +
			"whether the market is open, and the quote timestamp. Use for 'what is X trading at', 'price of X', 'how is X doing today'. " +
			"Requires a ticker; if the user gave a company name, call search_symbol first. The UI shows a quote card automatically.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string","description":"US ticker, e.g. AAPL"}},"required":["symbol"],"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *GetQuote) Mutating() bool { return false }

// Label implements Labeler.
func (t *GetQuote) Label(input json.RawMessage) string {
	var in symbolInput
	_ = json.Unmarshal(input, &in)
	if s := strings.ToUpper(strings.TrimSpace(in.Symbol)); symbolRe.MatchString(s) {
		return fmt.Sprintf("Fetching %s quote…", s)
	}
	return "Fetching quote…"
}

// Execute implements Tool.
func (t *GetQuote) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var in symbolInput
	if err := decodeStrict(input, &in); err != nil {
		return Result{}, err
	}
	sym, err := NormalizeSymbol(in.Symbol)
	if err != nil {
		return Result{}, err
	}
	q, err := t.Market.Quote(ctx, sym)
	if err != nil {
		return Result{}, fmt.Errorf("get_quote %s: %w", sym, err)
	}
	q.MarketOpen = marketStatus(ctx, t.Market, t.Now())

	fm, err := marshalForModel(toQuoteForModel(q))
	if err != nil {
		return Result{}, err
	}
	ui, err := NewUIBlock(UIQuoteCard, q)
	if err != nil {
		return Result{}, err
	}
	return Result{ForModel: fm, UI: ui}, nil
}
