// Package market defines the market data Provider interface, its decorators
// (cache, rate limiter, composite), and helpers shared by implementations.
package market

import (
	"context"
	"errors"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

// Provider supplies market data. Implementations: finnhub, mock, and decorators.
type Provider interface {
	SearchSymbol(ctx context.Context, query string) ([]domain.SymbolMatch, error)
	Quote(ctx context.Context, symbol string) (domain.Quote, error)
	History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error)
	Profile(ctx context.Context, symbol string) (domain.CompanyProfile, error)
	News(ctx context.Context, symbol string, from, to time.Time, limit int) ([]domain.NewsItem, error)
	MarketOpen(ctx context.Context) (bool, error)
}

// HistorySource is the subset of Provider needed for price history. It lets a
// history-only provider (e.g. Twelve Data) plug into a Composite.
type HistorySource interface {
	History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error)
}

// Typed errors returned by providers. Wrap them with context; callers use errors.Is.
var (
	ErrNotFound    = errors.New("not found")
	ErrRateLimited = errors.New("rate limited by market data provider")
	ErrNoAccess    = errors.New("market data endpoint not available on this plan")
	ErrUpstream    = errors.New("market data provider error")
)

// Composite serves History from a separate source and everything else from Primary.
type Composite struct {
	Provider
	HistoryFrom HistorySource
}

// NewComposite returns primary with History delegated to history. If history is
// nil, primary is returned unchanged.
func NewComposite(primary Provider, history HistorySource) Provider {
	if history == nil {
		return primary
	}
	return &Composite{Provider: primary, HistoryFrom: history}
}

// History implements Provider.
func (c *Composite) History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error) {
	return c.HistoryFrom.History(ctx, symbol, r)
}
