package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// MaxWatchlist caps the watchlist size.
const MaxWatchlist = 50

// WatchlistStore persists the watchlist.
type WatchlistStore interface {
	WatchlistSymbols(ctx context.Context) ([]string, error)
	AddToWatchlist(ctx context.Context, symbol string) (bool, error)
	RemoveFromWatchlist(ctx context.Context, symbol string) (bool, error)
}

// QuoteList is the quote_list UI payload.
type QuoteList struct {
	Quotes  []domain.Quote `json:"quotes"`
	Missing []string       `json:"missing,omitempty"`
}

// FetchQuotes gets quotes for symbols concurrently (bounded). Symbols that fail
// are returned in missing. Order follows symbols.
func FetchQuotes(ctx context.Context, p market.Provider, symbols []string, now time.Time) ([]domain.Quote, []string) {
	quotes := make([]*domain.Quote, len(symbols))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i, s := range symbols {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if q, err := p.Quote(ctx, s); err == nil {
				quotes[i] = &q
			}
		}()
	}
	wg.Wait()
	open := marketStatus(ctx, p, now)
	var out []domain.Quote
	var missing []string
	for i, q := range quotes {
		if q == nil {
			missing = append(missing, symbols[i])
			continue
		}
		q.MarketOpen = open
		out = append(out, *q)
	}
	if out == nil {
		out = []domain.Quote{}
	}
	return out, missing
}

// ListWatchlist is the list_watchlist tool.
type ListWatchlist struct {
	Store  WatchlistStore
	Market market.Provider
	Now    func() time.Time
}

// Spec implements Tool.
func (t *ListWatchlist) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "list_watchlist",
		Description: "Show the user's watchlist with the latest quote for each symbol. Use for 'my watchlist', 'what am I watching', 'how are my stocks doing'.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *ListWatchlist) Mutating() bool { return false }

// Label implements Labeler.
func (t *ListWatchlist) Label(json.RawMessage) string { return "Loading your watchlist…" }

// Execute implements Tool.
func (t *ListWatchlist) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	if err := decodeStrict(input, &struct{}{}); err != nil {
		return Result{}, err
	}
	syms, err := t.Store.WatchlistSymbols(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list_watchlist: %w", err)
	}
	quotes, missing := FetchQuotes(ctx, t.Market, syms, t.Now())
	rows := make([]quoteForModel, len(quotes))
	for i, q := range quotes {
		rows[i] = toQuoteForModel(q)
		rows[i].Note = ""
	}
	out := map[string]any{"symbols": syms, "quotes": rows}
	if len(syms) == 0 {
		out["note"] = "The watchlist is empty. The user can ask to add symbols."
	}
	if len(missing) > 0 {
		out["unavailable"] = missing
	}
	fm, err := marshalForModel(out)
	if err != nil {
		return Result{}, err
	}
	ui, err := NewUIBlock(UIQuoteList, QuoteList{Quotes: quotes, Missing: missing})
	if err != nil {
		return Result{}, err
	}
	return Result{ForModel: fm, UI: ui}, nil
}

// WatchlistChange is watchlist_add or watchlist_remove (mutating).
type WatchlistChange struct {
	Store  WatchlistStore
	Market market.Provider
	Remove bool
}

// Spec implements Tool.
func (t *WatchlistChange) Spec() llm.ToolSpec {
	if t.Remove {
		return llm.ToolSpec{
			Name: "watchlist_remove",
			Description: "Remove one ticker from the user's watchlist. Call once per symbol. " +
				"Takes effect only after the user clicks Confirm in the UI; tell them it is waiting for confirmation.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string","description":"US ticker on the watchlist"}},"required":["symbol"],"additionalProperties":false}`),
		}
	}
	return llm.ToolSpec{
		Name: "watchlist_add",
		Description: "Add one US ticker to the user's watchlist. Call once per symbol (several calls for several symbols). " +
			"Takes effect only after the user clicks Confirm in the UI; tell them it is waiting for confirmation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string","description":"US ticker, e.g. NVDA"}},"required":["symbol"],"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *WatchlistChange) Mutating() bool { return true }

// Label implements Labeler.
func (t *WatchlistChange) Label(input json.RawMessage) string {
	var in symbolInput
	_ = json.Unmarshal(input, &in)
	sym := strings.ToUpper(strings.TrimSpace(in.Symbol))
	if !symbolRe.MatchString(sym) {
		sym = "symbol"
	}
	if t.Remove {
		return fmt.Sprintf("Preparing to remove %s from watchlist…", sym)
	}
	return fmt.Sprintf("Preparing to add %s to watchlist…", sym)
}

func (t *WatchlistChange) validate(ctx context.Context, input json.RawMessage) (string, error) {
	var in symbolInput
	if err := decodeStrict(input, &in); err != nil {
		return "", err
	}
	sym, err := NormalizeSymbol(in.Symbol)
	if err != nil {
		return "", err
	}
	syms, err := t.Store.WatchlistSymbols(ctx)
	if err != nil {
		return "", fmt.Errorf("watchlist: %w", err)
	}
	on := false
	for _, s := range syms {
		if s == sym {
			on = true
		}
	}
	if t.Remove {
		if !on {
			return "", inputErr("%s is not on the watchlist", sym)
		}
		return sym, nil
	}
	if on {
		return "", inputErr("%s is already on the watchlist", sym)
	}
	if len(syms) >= MaxWatchlist {
		return "", inputErr("the watchlist is full (%d symbols); remove one first", MaxWatchlist)
	}
	// Verify the ticker exists before offering to add it.
	if _, err := t.Market.Quote(ctx, sym); err != nil {
		if errors.Is(err, market.ErrNotFound) {
			return "", inputErr("%s doesn't look like a valid US ticker; use search_symbol", sym)
		}
		return "", fmt.Errorf("verify %s: %w", sym, err)
	}
	return sym, nil
}

// Prepare implements Preparer.
func (t *WatchlistChange) Prepare(ctx context.Context, input json.RawMessage) (string, json.RawMessage, error) {
	sym, err := t.validate(ctx, input)
	if err != nil {
		return "", nil, err
	}
	norm, _ := json.Marshal(symbolInput{Symbol: sym})
	if t.Remove {
		return fmt.Sprintf("Remove %s from your watchlist", sym), norm, nil
	}
	return fmt.Sprintf("Add %s to your watchlist", sym), norm, nil
}

// Execute implements Tool. Only called after user confirmation.
func (t *WatchlistChange) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	sym, err := t.validate(ctx, input)
	if err != nil {
		return Result{}, err
	}
	if t.Remove {
		if _, err := t.Store.RemoveFromWatchlist(ctx, sym); err != nil {
			return Result{}, fmt.Errorf("watchlist_remove: %w", err)
		}
		return Result{ForModel: fmt.Sprintf("%s removed from the watchlist", sym)}, nil
	}
	if _, err := t.Store.AddToWatchlist(ctx, sym); err != nil {
		return Result{}, fmt.Errorf("watchlist_add: %w", err)
	}
	return Result{ForModel: fmt.Sprintf("%s added to the watchlist", sym)}, nil
}
