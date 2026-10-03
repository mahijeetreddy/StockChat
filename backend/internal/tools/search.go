package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// SearchSymbol is the search_symbol tool.
type SearchSymbol struct {
	Market market.Provider
}

type searchInput struct {
	Query string `json:"query"`
}

// Spec implements Tool.
func (t *SearchSymbol) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "search_symbol",
		Description: "Find US stock tickers by company name or partial ticker. Use whenever the user names a company " +
			"instead of a ticker (e.g. 'Apple', 'Costco'), or when unsure a ticker exists. Returns up to 5 matches. " +
			"If several different companies match plausibly, ask the user which one they mean.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Company name or ticker fragment, e.g. 'Apple'"}},"required":["query"],"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *SearchSymbol) Mutating() bool { return false }

// Label implements Labeler.
func (t *SearchSymbol) Label(input json.RawMessage) string {
	var in searchInput
	_ = json.Unmarshal(input, &in)
	if q := strings.TrimSpace(in.Query); q != "" && utf8.RuneCountInString(q) <= 40 {
		return fmt.Sprintf("Searching for “%s”…", q)
	}
	return "Searching symbols…"
}

// Execute implements Tool.
func (t *SearchSymbol) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var in searchInput
	if err := decodeStrict(input, &in); err != nil {
		return Result{}, err
	}
	q := strings.TrimSpace(in.Query)
	if q == "" || utf8.RuneCountInString(q) > 64 {
		return Result{}, inputErr("query must be 1-64 characters")
	}
	matches, err := t.Market.SearchSymbol(ctx, q)
	if err != nil {
		return Result{}, fmt.Errorf("search_symbol: %w", err)
	}
	if len(matches) > 5 {
		matches = matches[:5]
	}
	out := struct {
		Matches []domain.SymbolMatch `json:"matches"`
		Note    string               `json:"note,omitempty"`
	}{Matches: matches}
	if len(matches) == 0 {
		out.Matches = []domain.SymbolMatch{}
		out.Note = "no US listings matched; ask the user to clarify the company or ticker"
	}
	fm, err := marshalForModel(out)
	if err != nil {
		return Result{}, err
	}
	return Result{ForModel: fm}, nil
}
