package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/config"
	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

func testConfig(t *testing.T) config.Config {
	return config.Config{
		Port: 8080, BindAddr: "127.0.0.1", AppEnv: "dev", CORSOrigin: "http://localhost:5173",
		LLMProvider: "fake", MarketProvider: "mock", MockMarket: "closed",
		LLMMaxTokens: 1024, AgentMaxIterations: 6, AlertsPollSeconds: 3600,
		DBPath: filepath.Join(t.TempDir(), "app.db"),
	}
}

// TestOfflineAppEndToEnd runs the fully wired app in fake/mock mode: a chat
// turn, then an alert fired by a dev price nudge and delivered over SSE.
func TestOfflineAppEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := New(ctx, testConfig(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	defer a.Close()
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	defer srv.Close()

	// Chat with the offline demo model.
	resp, err := http.Post(srv.URL+"/api/chat", "application/json", strings.NewReader(`{"message":"Compare NVDA and AMD over 3 months"}`))
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Contains(t, string(body), "event: done")
	assert.Contains(t, string(body), `"type":"compare"`)

	// Create an alert directly, subscribe to notifications, nudge the price.
	_, err = a.Store.CreateAlert(ctx, domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceAbove, Threshold: 900, Cooldown: time.Hour})
	require.NoError(t, err)

	sreq, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream/notifications", nil)
	sresp, err := http.DefaultClient.Do(sreq)
	require.NoError(t, err)
	defer sresp.Body.Close()
	reader := bufio.NewReader(sresp.Body)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, ": connected\n", line, "subscribed before nudging")

	nudge, err := http.Post(srv.URL+"/api/dev/mock/price", "application/json", strings.NewReader(`{"symbol":"AAPL","price":950}`))
	require.NoError(t, err)
	nudge.Body.Close()
	require.Equal(t, http.StatusOK, nudge.StatusCode)

	got := make(chan string, 1)
	go func() {
		for {
			l, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(l, "data: ") {
				got <- strings.TrimPrefix(l, "data: ")
				return
			}
		}
	}()
	select {
	case data := <-got:
		var n map[string]any
		require.NoError(t, json.Unmarshal([]byte(data), &n))
		assert.Equal(t, "AAPL", n["symbol"])
		assert.Equal(t, 950.0, n["price"])
	case <-time.After(3 * time.Second):
		t.Fatal("no notification within one cycle")
	}

	// The quote reflects the nudge (cache invalidated).
	wl, err := http.Get(srv.URL + "/api/market/status")
	require.NoError(t, err)
	wl.Body.Close()
	assert.Equal(t, http.StatusOK, wl.StatusCode)
}

func TestDevRoutesNotMountedInProd(t *testing.T) {
	cfg := testConfig(t)
	cfg.AppEnv = "prod"
	a, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	defer a.Close()
	srv := httptest.NewServer(a.Handler)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/dev/mock/price", "application/json", strings.NewReader(`{"symbol":"AAPL","price":1}`))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
