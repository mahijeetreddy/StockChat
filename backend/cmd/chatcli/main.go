// Command chatcli is a developer REPL to chat with the agent in the terminal
// (in-memory history, same LLM and market wiring as the server).
//
//	LLM_PROVIDER=fake MARKET_PROVIDER=mock go run ./cmd/chatcli
//	go run ./cmd/chatcli "what's Apple's price?"   # one-shot
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/app"
	"github.com/mahijeetreddy/stockchat/backend/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(".env", "../.env"); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	m, err := app.NewMarket(cfg)
	if err != nil {
		return err
	}
	client, err := app.NewLLM(ctx, cfg)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a := &agent.Agent{
		LLM:           client,
		Tools:         app.NewTools(app.ToolDeps{Market: m.Provider}),
		History:       agent.NewMemoryHistory(),
		MaxIterations: cfg.AgentMaxIterations,
		MaxTokens:     cfg.LLMMaxTokens,
		Logger:        logger,
	}
	loc, _ := time.LoadLocation("Local")

	turn := func(text string) {
		err := a.Run(ctx, agent.RunInput{ConversationID: "cli", UserText: text, Location: loc}, printEvent)
		fmt.Println()
		if err != nil {
			logger.Debug("turn failed", "err", err)
		}
	}

	if len(os.Args) > 1 {
		turn(strings.Join(os.Args[1:], " "))
		return nil
	}
	fmt.Printf("StockChat CLI (llm=%s, market=%s). Ctrl+C to quit.\n", cfg.LLMProvider, cfg.MarketProvider)
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !sc.Scan() || ctx.Err() != nil {
			return nil
		}
		if text := strings.TrimSpace(sc.Text()); text != "" {
			turn(text)
		}
	}
}

func printEvent(ev agent.Event) {
	switch d := ev.Data.(type) {
	case agent.TextDeltaData:
		fmt.Print(d.Text)
	case agent.ToolStartData:
		fmt.Printf("\n  [%s] %s\n", d.Name, d.Label)
	case agent.ToolResultData:
		if d.OK {
			ui := ""
			if d.UI != nil {
				b, _ := json.Marshal(d.UI.Data)
				ui = fmt.Sprintf(" ui=%s %s", d.UI.Type, truncate(string(b), 160))
			}
			fmt.Printf("  [%s] ok%s\n", d.Name, ui)
		} else {
			fmt.Printf("  [%s] error: %s\n", d.Name, d.Error)
		}
	case agent.ConfirmData:
		fmt.Printf("  [confirm] %s (action %s)\n", d.Summary, d.ActionID)
	case agent.ErrorData:
		fmt.Printf("\n  error: %s (retryable=%v)\n", d.Message, d.Retryable)
	case agent.DoneData:
		fmt.Printf("\n  (tokens in=%d out=%d)", d.Usage.InputTokens, d.Usage.OutputTokens)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
