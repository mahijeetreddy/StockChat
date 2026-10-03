package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/config"
	"github.com/mahijeetreddy/stockchat/backend/internal/httpapi"
	"github.com/mahijeetreddy/stockchat/backend/internal/store"
)

// App is the fully wired server application.
type App struct {
	Cfg     config.Config
	Store   *store.Store
	Market  *Market
	Agent   *agent.Agent
	Server  *httpapi.Server
	Handler http.Handler
	log     *slog.Logger
}

// New wires every component from cfg.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	mkt, err := NewMarket(cfg)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	client, err := NewLLM(ctx, cfg)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	ag := &agent.Agent{
		LLM:           client,
		Tools:         NewTools(ToolDeps{Market: mkt.Provider}),
		History:       st,
		Actions:       st,
		MaxIterations: cfg.AgentMaxIterations,
		MaxTokens:     cfg.LLMMaxTokens,
		Logger:        logger,
	}
	srv := httpapi.New(st, ag, mkt.Provider, logger, httpapi.Options{
		CORSOrigin: cfg.CORSOrigin,
		AppToken:   cfg.AppToken,
	})
	a := &App{Cfg: cfg, Store: st, Market: mkt, Agent: ag, Server: srv, log: logger}
	a.Handler = srv.Handler()
	return a, nil
}

// Start launches background workers; they stop when ctx is cancelled.
func (a *App) Start(ctx context.Context) {
	go a.expireActions(ctx)
}

// Close releases resources.
func (a *App) Close() error { return a.Store.Close() }

func (a *App) expireActions(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := a.Store.ExpirePendingActions(ctx); err != nil {
				a.log.Warn("expire pending actions", "err", err)
			} else if n > 0 {
				a.log.Info("expired pending actions", "count", n)
			}
		}
	}
}
