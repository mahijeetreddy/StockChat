package alerts

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// Clock abstracts time for tests.
type Clock interface{ Now() time.Time }

// SystemClock is the real clock.
type SystemClock struct{}

// Now implements Clock.
func (SystemClock) Now() time.Time { return time.Now() }

// Store is the persistence the engine needs.
type Store interface {
	ListAlerts(ctx context.Context, activeOnly bool) ([]domain.Alert, error)
	RecordAlertFire(ctx context.Context, a domain.Alert, price float64, at time.Time) (int64, bool, error)
	MarkEventDelivered(ctx context.Context, eventID int64) error
}

// Engine polls quotes for symbols with active alerts and fires notifications.
type Engine struct {
	Store    Store
	Market   market.Provider
	Notifier Notifier // external delivery (log, Discord)
	Hub      *Hub     // live UI subscribers (may be nil)
	Clock    Clock
	Interval time.Duration
	// AlwaysOn evaluates outside market hours too (testing/demos).
	AlwaysOn bool
	// MaxSymbolsPerCycle bounds quote requests per cycle; extra symbols are
	// rotated through on later cycles.
	MaxSymbolsPerCycle int
	Logger             *slog.Logger

	mu      sync.Mutex
	offset  int
	trigger chan struct{}
}

func (e *Engine) init() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.trigger == nil {
		e.trigger = make(chan struct{}, 1)
	}
	if e.Clock == nil {
		e.Clock = SystemClock{}
	}
	if e.Interval <= 0 {
		e.Interval = 15 * time.Second
	}
	if e.MaxSymbolsPerCycle <= 0 {
		e.MaxSymbolsPerCycle = 20
	}
	if e.Logger == nil {
		e.Logger = slog.Default()
	}
	if e.Notifier == nil {
		e.Notifier = LogNotifier{Logger: e.Logger}
	}
}

// Trigger requests an immediate forced cycle (ignores market hours). Used by
// the dev-only price nudge so demos don't wait for the next tick.
func (e *Engine) Trigger() {
	e.init()
	select {
	case e.trigger <- struct{}{}:
	default:
	}
}

// Run polls until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	e.init()
	t := time.NewTicker(e.Interval)
	defer t.Stop()
	e.Logger.Info("alert engine started", "interval", e.Interval, "always_on", e.AlwaysOn)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Cycle(ctx, false)
		case <-e.trigger:
			e.Cycle(ctx, true)
		}
	}
}

// Cycle runs one evaluation pass and returns the notifications fired. force
// skips the market-hours check.
func (e *Engine) Cycle(ctx context.Context, force bool) []Notification {
	e.init()
	now := e.Clock.Now()
	if !force && !e.AlwaysOn && !e.marketOpen(ctx, now) {
		return nil
	}
	active, err := e.Store.ListAlerts(ctx, true)
	if err != nil {
		e.Logger.Warn("alert engine: load alerts", "err", err)
		return nil
	}
	if len(active) == 0 {
		return nil
	}
	bySymbol := map[string][]domain.Alert{}
	for _, a := range active {
		bySymbol[a.Symbol] = append(bySymbol[a.Symbol], a)
	}
	symbols := make([]string, 0, len(bySymbol))
	for s := range bySymbol {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	symbols = e.rotate(symbols)

	var fired []Notification
	for _, sym := range symbols {
		if ctx.Err() != nil {
			return fired
		}
		qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		q, err := e.Market.Quote(qctx, sym)
		cancel()
		if err != nil {
			e.Logger.Warn("alert engine: quote", "symbol", sym, "err", err)
			continue
		}
		for _, a := range bySymbol[sym] {
			if !Evaluate(a, q, now) {
				continue
			}
			evID, ok, err := e.Store.RecordAlertFire(ctx, a, q.Price, now)
			if err != nil {
				e.Logger.Warn("alert engine: record fire", "alert", a.ID, "err", err)
				continue
			}
			if !ok {
				continue // deleted or fired concurrently
			}
			n := NewNotification(a, q, now)
			fired = append(fired, n)
			if e.Hub != nil {
				e.Hub.Broadcast(n)
			}
			nctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = e.Notifier.Notify(nctx, n)
			cancel()
			if err != nil {
				e.Logger.Warn("alert engine: notify", "alert", a.ID, "err", err)
				continue
			}
			if err := e.Store.MarkEventDelivered(ctx, evID); err != nil {
				e.Logger.Warn("alert engine: mark delivered", "err", err)
			}
		}
	}
	return fired
}

// rotate returns at most MaxSymbolsPerCycle symbols, advancing a cursor so
// every symbol is checked over successive cycles.
func (e *Engine) rotate(symbols []string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := len(symbols)
	if n <= e.MaxSymbolsPerCycle {
		return symbols
	}
	start := e.offset % n
	out := make([]string, 0, e.MaxSymbolsPerCycle)
	for i := range e.MaxSymbolsPerCycle {
		out = append(out, symbols[(start+i)%n])
	}
	e.offset = (start + e.MaxSymbolsPerCycle) % n
	return out
}

func (e *Engine) marketOpen(ctx context.Context, now time.Time) bool {
	open, err := e.Market.MarketOpen(ctx)
	if err != nil {
		return market.IsRegularHours(now)
	}
	return open
}
