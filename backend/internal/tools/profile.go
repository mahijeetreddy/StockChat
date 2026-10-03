package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// GetProfile is the get_company_profile tool.
type GetProfile struct {
	Market market.Provider
}

// Spec implements Tool.
func (t *GetProfile) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "get_company_profile",
		Description: "Get basic company information for a US ticker: name, industry, exchange, country, market cap, website. " +
			"Use for 'tell me about X', 'what does X do', 'how big is X'. The UI shows a profile card automatically.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string","description":"US ticker, e.g. COST"}},"required":["symbol"],"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *GetProfile) Mutating() bool { return false }

// Label implements Labeler.
func (t *GetProfile) Label(input json.RawMessage) string {
	var in symbolInput
	_ = json.Unmarshal(input, &in)
	if s := strings.ToUpper(strings.TrimSpace(in.Symbol)); symbolRe.MatchString(s) {
		return fmt.Sprintf("Looking up %s company profile…", s)
	}
	return "Looking up company profile…"
}

// Execute implements Tool.
func (t *GetProfile) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var in symbolInput
	if err := decodeStrict(input, &in); err != nil {
		return Result{}, err
	}
	sym, err := NormalizeSymbol(in.Symbol)
	if err != nil {
		return Result{}, err
	}
	p, err := t.Market.Profile(ctx, sym)
	if err != nil {
		return Result{}, fmt.Errorf("get_company_profile %s: %w", sym, err)
	}
	// Provider text is untrusted: trim and strip control characters.
	p.Name = cleanText(p.Name, 120)
	p.Industry = cleanText(p.Industry, 80)
	p.Exchange = cleanText(p.Exchange, 80)
	p.Country = cleanText(p.Country, 40)
	p.WebURL = safeURL(p.WebURL)
	p.LogoURL = safeURL(p.LogoURL)

	fm, err := marshalForModel(map[string]any{
		"symbol":         sym,
		"name":           p.Name,
		"industry":       p.Industry,
		"exchange":       p.Exchange,
		"country":        p.Country,
		"market_cap_usd": humanUSD(p.MarketCap),
		"website":        p.WebURL,
	})
	if err != nil {
		return Result{}, err
	}
	ui, err := NewUIBlock(UIProfileCard, p)
	if err != nil {
		return Result{}, err
	}
	return Result{ForModel: fm, UI: ui}, nil
}

func humanUSD(v float64) string {
	switch {
	case v <= 0:
		return "unknown"
	case v >= 1e12:
		return fmt.Sprintf("$%.2fT", v/1e12)
	case v >= 1e9:
		return fmt.Sprintf("$%.2fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("$%.2fM", v/1e6)
	default:
		return fmt.Sprintf("$%.0f", v)
	}
}
