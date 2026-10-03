package twelvedata

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

func serve(t *testing.T, file string, status int) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "apikey k", r.Header.Get("Authorization"))
		assert.Empty(t, r.URL.Query().Get("apikey"), "key must not be in the URL")
		body, err := os.ReadFile(filepath.Join("testdata", file))
		require.NoError(t, err)
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return New("k", WithBaseURL(srv.URL))
}

func TestDaily(t *testing.T) {
	c := serve(t, "daily_AAPL.json", http.StatusOK)
	got, err := c.History(context.Background(), "AAPL", domain.Range1M)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.True(t, got[0].Time.Before(got[2].Time), "ascending")
	assert.Equal(t, 227.52, got[2].Close)
	assert.Equal(t, 45000000.0, got[2].Volume)
	assert.Equal(t, 9, got[2].Time.In(market.NewYork).Hour())
}

func TestIntradayKeepsLastSession(t *testing.T) {
	c := serve(t, "intraday_AAPL.json", http.StatusOK)
	got, err := c.History(context.Background(), "AAPL", domain.Range1D)
	require.NoError(t, err)
	require.Len(t, got, 2, "bars from the previous day are dropped")
	assert.Equal(t, time.Date(2026, 10, 2, 15, 50, 0, 0, market.NewYork).UTC(), got[0].Time)
}

func TestErrors(t *testing.T) {
	tests := []struct {
		file   string
		status int
		want   error
	}{
		{"error_429.json", http.StatusOK, market.ErrRateLimited}, // error in body with HTTP 200
		{"error_404.json", http.StatusOK, market.ErrNotFound},
		{"error_401.json", http.StatusUnauthorized, market.ErrNoAccess},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			_, err := serve(t, tt.file, tt.status).History(context.Background(), "AAPL", domain.Range1M)
			assert.ErrorIs(t, err, tt.want)
		})
	}
}
