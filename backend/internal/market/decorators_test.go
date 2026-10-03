package market

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

// countingProvider counts upstream calls and can block or fail on demand.
type countingProvider struct {
	quotes  atomic.Int32
	history atomic.Int32
	delay   time.Duration
	fail    atomic.Bool
}

func (p *countingProvider) SearchSymbol(context.Context, string) ([]domain.SymbolMatch, error) {
	return nil, nil
}

func (p *countingProvider) Quote(ctx context.Context, s string) (domain.Quote, error) {
	p.quotes.Add(1)
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	if p.fail.Load() {
		return domain.Quote{}, ErrUpstream
	}
	return domain.Quote{Symbol: s, Price: 100}, nil
}

func (p *countingProvider) History(_ context.Context, _ string, _ domain.HistoryRange) ([]domain.Candle, error) {
	p.history.Add(1)
	return []domain.Candle{{Close: 1}}, nil
}

func (p *countingProvider) Profile(context.Context, string) (domain.CompanyProfile, error) {
	return domain.CompanyProfile{}, nil
}

func (p *countingProvider) News(context.Context, string, time.Time, time.Time, int) ([]domain.NewsItem, error) {
	return nil, nil
}

func (p *countingProvider) MarketOpen(context.Context) (bool, error) { return true, nil }

func TestCacheSingleflight(t *testing.T) {
	up := &countingProvider{delay: 50 * time.Millisecond}
	c := WithCache(up, DefaultCacheTTLs())

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, err := c.Quote(context.Background(), "AAPL")
			assert.NoError(t, err)
			assert.Equal(t, 100.0, q.Price)
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), up.quotes.Load(), "10 concurrent identical calls must hit upstream once")

	// A different symbol is a different key.
	_, err := c.Quote(context.Background(), "MSFT")
	require.NoError(t, err)
	assert.Equal(t, int32(2), up.quotes.Load())
}

func TestCacheTTLExpiry(t *testing.T) {
	up := &countingProvider{}
	c := WithCache(up, DefaultCacheTTLs())
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	ctx := context.Background()
	_, _ = c.Quote(ctx, "AAPL")
	_, _ = c.Quote(ctx, "AAPL")
	assert.Equal(t, int32(1), up.quotes.Load(), "second call served from cache")

	now = now.Add(11 * time.Second) // quote TTL is 10s
	_, _ = c.Quote(ctx, "AAPL")
	assert.Equal(t, int32(2), up.quotes.Load(), "expired entry refetched")

	// Intraday history uses the short TTL, daily the long one.
	_, _ = c.History(ctx, "AAPL", domain.Range1D)
	_, _ = c.History(ctx, "AAPL", domain.Range1Y)
	now = now.Add(2 * time.Minute)
	_, _ = c.History(ctx, "AAPL", domain.Range1D)
	_, _ = c.History(ctx, "AAPL", domain.Range1Y)
	assert.Equal(t, int32(3), up.history.Load(), "1D refetched after 1m, 1Y still cached")
}

func TestCacheDoesNotCacheErrors(t *testing.T) {
	up := &countingProvider{}
	up.fail.Store(true)
	c := WithCache(up, DefaultCacheTTLs())
	_, err := c.Quote(context.Background(), "AAPL")
	require.ErrorIs(t, err, ErrUpstream)

	up.fail.Store(false)
	q, err := c.Quote(context.Background(), "AAPL")
	require.NoError(t, err)
	assert.Equal(t, 100.0, q.Price)
	assert.Equal(t, int32(2), up.quotes.Load())
}

func TestCacheRespectsCallerCancellation(t *testing.T) {
	up := &countingProvider{delay: 200 * time.Millisecond}
	c := WithCache(up, DefaultCacheTTLs())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Quote(ctx, "AAPL")
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestLimiterSpacesRequests(t *testing.T) {
	up := &countingProvider{}
	l := WithLimiter(up, 20, 1) // one request every 50ms
	start := time.Now()
	for range 5 {
		_, err := l.Quote(context.Background(), "AAPL")
		require.NoError(t, err)
	}
	// First call is immediate; the remaining 4 wait ~50ms each.
	assert.GreaterOrEqual(t, time.Since(start), 190*time.Millisecond)
	assert.Equal(t, int32(5), up.quotes.Load())
}

func TestLimiterHonoursContext(t *testing.T) {
	l := WithLimiter(&countingProvider{}, 0.1, 1) // one request per 10s
	_, err := l.Quote(context.Background(), "AAPL")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = l.Quote(ctx, "AAPL")
	assert.Error(t, err, "waiting past the deadline must fail fast")
}

func TestComposite(t *testing.T) {
	primary := &countingProvider{}
	hist := &countingProvider{}
	p := NewComposite(primary, hist)
	_, _ = p.History(context.Background(), "AAPL", domain.Range1M)
	_, _ = p.Quote(context.Background(), "AAPL")
	assert.Equal(t, int32(1), hist.history.Load())
	assert.Equal(t, int32(0), primary.history.Load())
	assert.Equal(t, int32(1), primary.quotes.Load())
	assert.Same(t, primary, NewComposite(primary, nil))
}
