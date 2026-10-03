// Package httpapi exposes the REST + SSE API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/alerts"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
	"github.com/mahijeetreddy/stockchat/backend/internal/store"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

// Runner runs one chat turn (implemented by *agent.Agent).
type Runner interface {
	Run(ctx context.Context, in agent.RunInput, emit func(agent.Event)) error
}

// Options configures the server.
type Options struct {
	CORSOrigin        string
	AppToken          string
	MaxBodyBytes      int64
	MaxMessageRunes   int
	ChatPerMinute     int
	HeartbeatInterval time.Duration
	DefaultLocation   *time.Location
}

// Deps are the server's collaborators.
type Deps struct {
	Store  *store.Store
	Agent  Runner
	Market market.Provider
	Tools  *tools.Registry // executes confirmed actions
	Hub    *alerts.Hub     // alert notifications for SSE subscribers (may be nil)
	Logger *slog.Logger
}

// Server holds handler dependencies.
type Server struct {
	store  *store.Store
	agent  Runner
	market market.Provider
	tools  *tools.Registry
	hub    *alerts.Hub
	log    *slog.Logger
	opts   Options
	now    func() time.Time

	busyMu sync.Mutex
	busy   map[string]bool     // conversations with a reply in progress
	notes  map[string][]string // system notes queued until the reply finishes

	extra []func(chi.Router) // extra routes (e.g. dev-only endpoints)
}

// New builds a Server.
func New(d Deps, opts Options) *Server {
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 32 << 10
	}
	if opts.MaxMessageRunes <= 0 {
		opts.MaxMessageRunes = 2000
	}
	if opts.ChatPerMinute <= 0 {
		opts.ChatPerMinute = 30
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 15 * time.Second
	}
	if opts.DefaultLocation == nil {
		opts.DefaultLocation = market.NewYork
	}
	return &Server{
		store: d.Store, agent: d.Agent, market: d.Market, tools: d.Tools, hub: d.Hub, log: d.Logger,
		opts: opts, now: time.Now, busy: map[string]bool{}, notes: map[string][]string{},
	}
}

// Handler returns the HTTP handler with all routes and middleware.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(accessLog(s.log))
	r.Use(middleware.Recoverer)
	r.Use(cors(s.opts.CORSOrigin))

	r.Get("/healthz", s.handleHealth)

	chatLimiter := newIPLimiter(s.opts.ChatPerMinute, 10)
	r.Route("/api", func(r chi.Router) {
		r.Use(bearerAuth(s.opts.AppToken))
		r.Use(maxBody(s.opts.MaxBodyBytes))

		r.With(chatLimiter.middleware).Post("/chat", s.handleChat)
		r.Get("/conversations", s.handleListConversations)
		r.Get("/conversations/{id}", s.handleGetConversation)
		r.Delete("/conversations/{id}", s.handleDeleteConversation)
		r.Get("/market/status", s.handleMarketStatus)
		r.Get("/watchlist", s.handleWatchlist)
		r.Get("/alerts", s.handleListAlerts)
		r.Delete("/alerts/{id}", s.handleDeleteAlert)
		r.Post("/actions/{id}/confirm", s.handleConfirmAction)
		r.Post("/actions/{id}/cancel", s.handleCancelAction)
		r.Get("/stream/notifications", s.handleNotifications)
		r.Get("/history/{symbol}", s.handleHistory)
		for _, f := range s.extra {
			f(r)
		}
	})
	return r
}

// Mount registers extra /api routes.
func (s *Server) Mount(f func(chi.Router)) { s.extra = append(s.extra, f) }

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMarketStatus(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	open, err := s.market.MarketOpen(r.Context())
	if err != nil {
		s.log.Warn("market status", "err", err)
		open = market.IsRegularHours(now)
	}
	resp := map[string]any{"open": open, "as_of": now.UTC()}
	if !open {
		resp["next_open"] = market.NextOpen(now).UTC()
	}
	writeJSON(w, http.StatusOK, resp)
}

// tryLock marks a conversation busy; false if a reply is already running.
func (s *Server) tryLock(id string) bool {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	if s.busy[id] {
		return false
	}
	s.busy[id] = true
	return true
}

// unlock releases a conversation and appends any system notes that arrived
// while the reply was streaming, so they never split a tool call from its result.
func (s *Server) unlock(id string) {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	delete(s.busy, id)
	for _, n := range s.notes[id] {
		s.appendNote(id, n)
	}
	delete(s.notes, id)
}

// addNote appends a system note now, or queues it if a reply is in progress.
func (s *Server) addNote(convoID, note string) {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	if s.busy[convoID] {
		s.notes[convoID] = append(s.notes[convoID], note)
		return
	}
	s.appendNote(convoID, note)
}

// appendNote must be called with busyMu held.
func (s *Server) appendNote(convoID, note string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.store.AppendMessage(ctx, convoID, llm.TextMessage(llm.RoleUser, note), nil); err != nil {
		s.log.Warn("append system note", "convo", convoID, "err", err)
	}
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON strictly decodes a request body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
