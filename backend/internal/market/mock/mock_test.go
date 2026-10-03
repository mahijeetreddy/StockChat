package mock

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// Friday 2026-10-02 11:00 ET (market open) and Saturday 2026-10-03 (closed).
var (
	friday   = time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	saturday = time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC)
)

func fixed(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestQuoteDeterministicAndOpen(t *testing.T) {
	ctx := context.Background()
	p := New(WithClock(fixed(friday)))
	q1, err := p.Quote(ctx, "AAPL")
	require.NoError(t, err)
	q2, err := New(WithClock(fixed(friday))).Quote(ctx, "AAPL")
	require.NoError(t, err)
	assert.Equal(t, q1, q2, "same symbol and clock must give same quote")
	assert.True(t, q1.MarketOpen)
	assert.InDelta(t, 232, q1.Price, 232*0.4)
	assert.LessOrEqual(t, q1.Low, q1.Price)
	assert.GreaterOrEqual(t, q1.High, q1.Price)
	assert.InDelta(t, q1.Price-q1.PrevClose, q1.Change, 0.011)
}

func TestQuoteWeekendShowsLastClose(t *testing.T) {
	p := New(WithClock(fixed(saturday)))
	q, err := p.Quote(context.Background(), "TSLA")
	require.NoError(t, err)
	assert.False(t, q.MarketOpen)
	assert.Equal(t, market.LastClose(saturday).UTC(), q.AsOf)
}

func TestMarketModeOverride(t *testing.T) {
	p := New(WithClock(fixed(saturday)), WithMarketMode(MarketOpen))
	open, err := p.MarketOpen(context.Background())
	require.NoError(t, err)
	assert.True(t, open)
	p.SetMarketMode(MarketClosed)
	open, _ = p.MarketOpen(context.Background())
	assert.False(t, open)
}

func TestSetPrice(t *testing.T) {
	p := New(WithClock(fixed(friday)))
	require.NoError(t, p.SetPrice("NVDA", 999.5))
	q, err := p.Quote(context.Background(), "NVDA")
	require.NoError(t, err)
	assert.Equal(t, 999.5, q.Price)
	require.ErrorIs(t, p.SetPrice("ZZZZ", 1), market.ErrNotFound)
}

func TestHistoryShapes(t *testing.T) {
	ctx := context.Background()
	for _, now := range []time.Time{friday, saturday} {
		p := New(WithClock(fixed(now)))
		for _, r := range domain.HistoryRanges {
			t.Run(now.Weekday().String()+"/"+string(r), func(t *testing.T) {
				candles, err := p.History(ctx, "MSFT", r)
				require.NoError(t, err)
				require.Len(t, candles, r.BarSpec().Count)
				for i, c := range candles {
					assert.LessOrEqual(t, c.Low, c.High)
					assert.False(t, c.Time.After(now), "no future bars")
					wd := c.Time.In(market.NewYork).Weekday()
					assert.NotEqual(t, time.Saturday, wd)
					assert.NotEqual(t, time.Sunday, wd)
					if i > 0 {
						assert.True(t, c.Time.After(candles[i-1].Time), "ascending")
					}
				}
			})
		}
	}
}

func TestSearch(t *testing.T) {
	p := New()
	tests := []struct {
		q     string
		first string
		n     int
	}{
		{"Apple", "AAPL", 2},
		{"aapl", "AAPL", 1},
		{"Delta", "DAL", 2},
		{"meta", "META", 2},
		{"asdfgh", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.q, func(t *testing.T) {
			got, err := p.SearchSymbol(context.Background(), tt.q)
			require.NoError(t, err)
			require.Len(t, got, tt.n)
			if tt.n > 0 {
				assert.Equal(t, tt.first, got[0].Symbol)
			}
		})
	}
}

func TestNewsAndProfile(t *testing.T) {
	ctx := context.Background()
	p := New(WithClock(fixed(friday)))
	p.AddNews("MSFT", domain.NewsItem{Headline: "Injected", PublishedAt: friday.Add(-time.Minute)})
	news, err := p.News(ctx, "MSFT", friday.Add(-7*24*time.Hour), friday, 3)
	require.NoError(t, err)
	require.Len(t, news, 3)
	assert.Equal(t, "Injected", news[0].Headline)

	prof, err := p.Profile(ctx, "COST")
	require.NoError(t, err)
	assert.Equal(t, "Costco Wholesale Corp", prof.Name)
	assert.Equal(t, 400e9, prof.MarketCap)

	_, err = p.Profile(ctx, "NOPE")
	assert.ErrorIs(t, err, market.ErrNotFound)
	_, err = p.Quote(ctx, "NOPE")
	assert.ErrorIs(t, err, market.ErrNotFound)
}
