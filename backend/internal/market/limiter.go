package market

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

// Limiter is a Provider decorator that spaces upstream calls to stay under a
// provider's rate limit. Callers wait (respecting ctx) rather than fail.
type Limiter struct {
	next Provider
	lim  *rate.Limiter
}

// WithLimiter wraps next so at most rps requests per second (with the given
// burst) reach it.
func WithLimiter(next Provider, rps float64, burst int) *Limiter {
	if burst < 1 {
		burst = 1
	}
	return &Limiter{next: next, lim: rate.NewLimiter(rate.Limit(rps), burst)}
}

func (l *Limiter) wait(ctx context.Context) error {
	if err := l.lim.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}
	return nil
}

// SearchSymbol implements Provider.
func (l *Limiter) SearchSymbol(ctx context.Context, query string) ([]domain.SymbolMatch, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	return l.next.SearchSymbol(ctx, query)
}

// Quote implements Provider.
func (l *Limiter) Quote(ctx context.Context, symbol string) (domain.Quote, error) {
	if err := l.wait(ctx); err != nil {
		return domain.Quote{}, err
	}
	return l.next.Quote(ctx, symbol)
}

// History implements Provider.
func (l *Limiter) History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	return l.next.History(ctx, symbol, r)
}

// Profile implements Provider.
func (l *Limiter) Profile(ctx context.Context, symbol string) (domain.CompanyProfile, error) {
	if err := l.wait(ctx); err != nil {
		return domain.CompanyProfile{}, err
	}
	return l.next.Profile(ctx, symbol)
}

// News implements Provider.
func (l *Limiter) News(ctx context.Context, symbol string, from, to time.Time, limit int) ([]domain.NewsItem, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	return l.next.News(ctx, symbol, from, to, limit)
}

// MarketOpen implements Provider.
func (l *Limiter) MarketOpen(ctx context.Context) (bool, error) {
	if err := l.wait(ctx); err != nil {
		return false, err
	}
	return l.next.MarketOpen(ctx)
}

// HistoryLimiter rate-limits a HistorySource (e.g. Twelve Data's 8 req/min free tier).
type HistoryLimiter struct {
	next HistorySource
	lim  *rate.Limiter
}

// WithHistoryLimiter wraps a HistorySource with a rate limiter.
func WithHistoryLimiter(next HistorySource, rps float64, burst int) *HistoryLimiter {
	if burst < 1 {
		burst = 1
	}
	return &HistoryLimiter{next: next, lim: rate.NewLimiter(rate.Limit(rps), burst)}
}

// History implements HistorySource.
func (l *HistoryLimiter) History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error) {
	if err := l.lim.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}
	return l.next.History(ctx, symbol, r)
}
