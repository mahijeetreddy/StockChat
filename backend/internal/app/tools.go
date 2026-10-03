package app

import (
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/market"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

// ToolStore is the persistence the tools need (implemented by *store.Store).
type ToolStore interface {
	tools.WatchlistStore
	tools.AlertStore
}

// ToolDeps are the dependencies tools need.
type ToolDeps struct {
	Market market.Provider
	Store  ToolStore // nil disables watchlist and alert tools (e.g. the CLI)
	Now    func() time.Time
}

// NewTools builds the tool registry.
func NewTools(d ToolDeps) *tools.Registry {
	if d.Now == nil {
		d.Now = time.Now
	}
	ts := []tools.Tool{
		&tools.SearchSymbol{Market: d.Market},
		&tools.GetQuote{Market: d.Market, Now: d.Now},
		&tools.GetHistory{Market: d.Market},
		&tools.CompareSymbols{Market: d.Market},
		&tools.GetProfile{Market: d.Market},
		&tools.GetNews{Market: d.Market, Now: d.Now},
	}
	if d.Store != nil {
		ts = append(ts,
			&tools.ListWatchlist{Store: d.Store, Market: d.Market, Now: d.Now},
			&tools.WatchlistChange{Store: d.Store, Market: d.Market},
			&tools.WatchlistChange{Store: d.Store, Market: d.Market, Remove: true},
			&tools.ListAlerts{Store: d.Store},
			&tools.CreateAlert{Store: d.Store, Market: d.Market},
			&tools.DeleteAlertTool{Store: d.Store},
		)
	}
	return tools.NewRegistry(ts...)
}
