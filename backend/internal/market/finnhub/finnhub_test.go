package finnhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

const testKey = "test-key-123"

// route maps "path?symbol" to a fixture file and status code.
type route struct {
	file   string
	status int
}

func newServer(t *testing.T, routes map[string]route) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The key must arrive in the header and never in the query string.
		assert.Equal(t, testKey, r.Header.Get("X-Finnhub-Token"))
		assert.NotContains(t, r.URL.RawQuery, testKey)
		assert.NotContains(t, r.URL.RawQuery, "token")

		key := r.URL.Path
		if s := r.URL.Query().Get("symbol"); s != "" {
			key += "?" + s
		} else if q := r.URL.Query().Get("q"); q != "" {
			key += "?" + q
		}
		rt, ok := routes[key]
		if !ok {
			t.Errorf("unexpected request %s", key)
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join("testdata", rt.file))
		require.NoError(t, err)
		if rt.status != 0 {
			w.WriteHeader(rt.status)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return New(testKey, WithBaseURL(srv.URL))
}

func TestQuote(t *testing.T) {
	c := newServer(t, map[string]route{
		"/quote?AAPL": {file: "quote_AAPL.json"},
		"/quote?ZZZZ": {file: "quote_unknown.json"},
	})
	q, err := c.Quote(context.Background(), "AAPL")
	require.NoError(t, err)
	assert.Equal(t, 227.52, q.Price)
	assert.Equal(t, 1.73, q.Change)
	assert.Equal(t, 225.79, q.PrevClose)
	assert.Equal(t, time.Unix(1759435200, 0).UTC(), q.AsOf)

	_, err = c.Quote(context.Background(), "ZZZZ")
	assert.ErrorIs(t, err, market.ErrNotFound)
}

func TestSearchFiltersNonUS(t *testing.T) {
	c := newServer(t, map[string]route{"/search?apple": {file: "search_apple.json"}})
	got, err := c.SearchSymbol(context.Background(), "apple")
	require.NoError(t, err)
	syms := make([]string, len(got))
	for i, m := range got {
		syms[i] = m.Symbol
	}
	assert.Equal(t, []string{"AAPL", "APLE", "AMAT"}, syms)
}

func TestProfile(t *testing.T) {
	c := newServer(t, map[string]route{
		"/stock/profile2?AAPL": {file: "profile_AAPL.json"},
		"/stock/profile2?ZZZZ": {file: "profile_unknown.json"},
	})
	p, err := c.Profile(context.Background(), "AAPL")
	require.NoError(t, err)
	assert.Equal(t, "Apple Inc", p.Name)
	assert.Equal(t, "Technology", p.Industry)
	assert.InDelta(t, 3.3812325e12, p.MarketCap, 1)

	_, err = c.Profile(context.Background(), "ZZZZ")
	assert.ErrorIs(t, err, market.ErrNotFound)
}

func TestNewsSortedLimitedSkipsEmpty(t *testing.T) {
	c := newServer(t, map[string]route{"/company-news?MSFT": {file: "news_MSFT.json"}})
	to := time.Unix(1759450000, 0)
	items, err := c.News(context.Background(), "MSFT", to.Add(-7*24*time.Hour), to, 10)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Microsoft shares edge higher", items[0].Headline, "newest first")

	items, err = c.News(context.Background(), "MSFT", to.Add(-7*24*time.Hour), to, 1)
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestMarketOpen(t *testing.T) {
	c := newServer(t, map[string]route{"/stock/market-status": {file: "market_status.json"}})
	open, err := c.MarketOpen(context.Background())
	require.NoError(t, err)
	assert.False(t, open)
}

func TestHistory(t *testing.T) {
	c := newServer(t, map[string]route{
		"/stock/candle?AAPL": {file: "candle_AAPL_D.json"},
		"/stock/candle?MSFT": {file: "candle_noaccess.json", status: http.StatusForbidden},
	})
	candles, err := c.History(context.Background(), "AAPL", domain.Range1M)
	require.NoError(t, err)
	require.Len(t, candles, 3)
	assert.Equal(t, 227.52, candles[2].Close)

	_, err = c.History(context.Background(), "MSFT", domain.Range1M)
	assert.ErrorIs(t, err, market.ErrNoAccess)
}

func TestStatusMapping(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusTooManyRequests, market.ErrRateLimited},
		{http.StatusUnauthorized, market.ErrNoAccess},
		{http.StatusForbidden, market.ErrNoAccess},
		{http.StatusNotFound, market.ErrNotFound},
		{http.StatusBadGateway, market.ErrUpstream},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			c := newServer(t, map[string]route{"/quote?AAPL": {file: "quote_AAPL.json", status: tt.status}})
			_, err := c.Quote(context.Background(), "AAPL")
			require.ErrorIs(t, err, tt.want)
			assert.NotContains(t, err.Error(), testKey)
		})
	}
}

func TestContextCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	c := New(testKey, WithBaseURL(srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Quote(ctx, "AAPL")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
