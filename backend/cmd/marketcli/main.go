// Command marketcli is a developer tool to try market data calls from the
// terminal using the same provider stack as the server.
//
//	go run ./cmd/marketcli quote AAPL
//	go run ./cmd/marketcli history AAPL 1M
//	go run ./cmd/marketcli search apple
//	go run ./cmd/marketcli profile COST
//	go run ./cmd/marketcli news MSFT
//	go run ./cmd/marketcli status
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/app"
	"github.com/mahijeetreddy/stockchat/backend/internal/config"
	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if err := config.LoadDotEnv(".env", "../.env"); err != nil {
		return err
	}
	// The CLI never needs an LLM; don't require Gemini settings.
	_ = os.Setenv("LLM_PROVIDER", "fake")
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	m, err := app.NewMarket(cfg)
	if err != nil {
		return err
	}
	p := m.Provider
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if len(args) == 0 {
		return fmt.Errorf("usage: marketcli quote|history|search|profile|news|status [args]")
	}
	arg := func(i int, def string) string {
		if len(args) > i {
			return args[i]
		}
		return def
	}
	sym := strings.ToUpper(arg(1, "AAPL"))

	var out any
	switch args[0] {
	case "quote":
		out, err = p.Quote(ctx, sym)
	case "history":
		var c []domain.Candle
		c, err = p.History(ctx, sym, domain.HistoryRange(strings.ToUpper(arg(2, "1M"))))
		if err == nil {
			fmt.Printf("%d candles\n", len(c))
			if len(c) > 5 {
				c = append(append([]domain.Candle{}, c[:2]...), c[len(c)-3:]...) // copy: cached slices are shared
			}
		}
		out = c
	case "search":
		out, err = p.SearchSymbol(ctx, arg(1, "apple"))
	case "profile":
		out, err = p.Profile(ctx, sym)
	case "news":
		now := time.Now()
		out, err = p.News(ctx, sym, now.AddDate(0, 0, -7), now, 5)
	case "status":
		out, err = p.MarketOpen(ctx)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
