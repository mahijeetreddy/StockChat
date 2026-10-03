// Package app wires concrete implementations together from Config. It is
// shared by the server and the developer CLIs.
package app

import (
	"fmt"

	"github.com/mahijeetreddy/stockchat/backend/internal/config"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
	"github.com/mahijeetreddy/stockchat/backend/internal/market/finnhub"
	"github.com/mahijeetreddy/stockchat/backend/internal/market/mock"
	"github.com/mahijeetreddy/stockchat/backend/internal/market/twelvedata"
)

// Market bundles the provider stack plus handles needed elsewhere.
type Market struct {
	Provider market.Provider // fully decorated provider to use everywhere
	Cache    *market.Cache   // for invalidation after demo price nudges
	Mock     *mock.Provider  // non-nil only in mock mode (demo controls)
}

// NewMarket builds: Cache -> Composite(Limiter -> primary, Limiter -> history).
// Caching sits outermost so cache hits never consume rate-limit tokens.
func NewMarket(cfg config.Config) (*Market, error) {
	var (
		primary market.Provider
		m       Market
	)
	switch cfg.MarketProvider {
	case "mock":
		m.Mock = mock.New(mock.WithMarketMode(mock.MarketMode(cfg.MockMarket)))
		primary = m.Mock
	case "finnhub":
		primary = market.WithLimiter(finnhub.New(cfg.FinnhubAPIKey), cfg.FinnhubRPS, 2)
	default:
		return nil, fmt.Errorf("unknown MARKET_PROVIDER %q", cfg.MarketProvider)
	}

	var hist market.HistorySource
	switch cfg.HistoryProvider {
	case "":
	case "mock":
		if m.Mock == nil {
			m.Mock = mock.New(mock.WithMarketMode(mock.MarketMode(cfg.MockMarket)))
		}
		hist = m.Mock
	case "twelvedata":
		hist = market.WithHistoryLimiter(twelvedata.New(cfg.TwelveDataKey), cfg.TwelveDataRPM/60, 2)
	default:
		return nil, fmt.Errorf("unknown HISTORY_PROVIDER %q", cfg.HistoryProvider)
	}

	m.Cache = market.WithCache(market.NewComposite(primary, hist), market.DefaultCacheTTLs())
	m.Provider = m.Cache
	return &m, nil
}
