package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mahijeetreddy/stockchat/backend/internal/market"
	"github.com/mahijeetreddy/stockchat/backend/internal/market/mock"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

// devRoutes are demo controls for the mock provider. They are only mounted
// when MARKET_PROVIDER=mock and APP_ENV=dev.
//
//	POST /api/dev/mock/price  {"symbol":"AAPL","price":260}   (price 0 clears)
//	POST /api/dev/mock/market {"mode":"open"|"closed"|"auto"}
func (a *App) devRoutes(r chi.Router) {
	r.Post("/dev/mock/price", a.handleMockPrice)
	r.Post("/dev/mock/market", a.handleMockMarket)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) handleMockPrice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Symbol string  `json:"symbol"`
		Price  float64 `json:"price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	sym, err := tools.NormalizeSymbol(in.Symbol)
	if err != nil || in.Price < 0 || in.Price > 1e6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need a valid symbol and price >= 0"})
		return
	}
	if err := a.Market.Mock.SetPrice(sym, in.Price); err != nil {
		if errors.Is(err, market.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "symbol not in the mock universe"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "couldn't set price"})
		return
	}
	a.Market.Cache.Invalidate()
	a.Engine.Trigger() // evaluate alerts now instead of on the next tick
	writeJSON(w, http.StatusOK, map[string]any{"symbol": sym, "price": in.Price})
}

func (a *App) handleMockMarket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	mode := mock.MarketMode(strings.ToLower(in.Mode))
	switch mode {
	case mock.MarketAuto, mock.MarketOpen, mock.MarketClosed:
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be auto, open, or closed"})
		return
	}
	a.Market.Mock.SetMarketMode(mode)
	a.Market.Cache.Invalidate()
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode})
}
