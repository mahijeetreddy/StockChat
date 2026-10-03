package app

import (
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/market"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

// ToolDeps are the dependencies tools need.
type ToolDeps struct {
	Market market.Provider
	Now    func() time.Time
}

// NewTools builds the tool registry.
func NewTools(d ToolDeps) *tools.Registry {
	if d.Now == nil {
		d.Now = time.Now
	}
	return tools.NewRegistry(
		&tools.SearchSymbol{Market: d.Market},
		&tools.GetQuote{Market: d.Market, Now: d.Now},
	)
}
