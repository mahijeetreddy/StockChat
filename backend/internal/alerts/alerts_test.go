package alerts

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/market/mock"
	"github.com/mahijeetreddy/stockchat/backend/internal/store"
)

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	q := func(price, pct float64) domain.Quote {
		return domain.Quote{Symbol: "AAPL", Price: price, ChangePercent: pct}
	}
	al := func(kind domain.AlertKind, th float64) domain.Alert {
		return domain.Alert{Symbol: "AAPL", Kind: kind, Threshold: th, Status: domain.AlertActive, Cooldown: time.Hour}
	}
	tests := []struct {
		name  string
		alert domain.Alert
		quote domain.Quote
		want  bool
	}{
		{"above: below threshold", al(domain.AlertPriceAbove, 250), q(249.99, 0), false},
		{"above: exactly equal fires", al(domain.AlertPriceAbove, 250), q(250, 0), true},
		{"above: over", al(domain.AlertPriceAbove, 250), q(251, 0), true},
		{"below: over threshold", al(domain.AlertPriceBelow, 200), q(200.01, 0), false},
		{"below: exactly equal fires", al(domain.AlertPriceBelow, 200), q(200, 0), true},
		{"below: under", al(domain.AlertPriceBelow, 200), q(150, 0), true},
		{"pct: up move", al(domain.AlertPercentChangeDay, 5), q(100, 5.2), true},
		{"pct: down move (absolute)", al(domain.AlertPercentChangeDay, 5), q(100, -5), true},
		{"pct: small move", al(domain.AlertPercentChangeDay, 5), q(100, -4.99), false},
		{"zero price never fires", al(domain.AlertPriceBelow, 200), q(0, 0), false},
		{"other symbol", al(domain.AlertPriceAbove, 1), domain.Quote{Symbol: "MSFT", Price: 5}, false},
		{"unknown kind", al("price_sideways", 1), q(5, 0), false},
		{"inactive", func() domain.Alert { a := al(domain.AlertPriceAbove, 1); a.Status = domain.AlertTriggered; return a }(), q(5, 0), false},
		{"once: already fired", func() domain.Alert { a := al(domain.AlertPriceAbove, 1); a.LastFired = ago(48 * time.Hour); return a }(), q(5, 0), false},
		{"repeat: within cooldown", func() domain.Alert {
			a := al(domain.AlertPriceAbove, 1)
			a.Repeat, a.LastFired = true, ago(59*time.Minute)
			return a
		}(), q(5, 0), false},
		{"repeat: cooldown elapsed", func() domain.Alert {
			a := al(domain.AlertPriceAbove, 1)
			a.Repeat, a.LastFired = true, ago(time.Hour)
			return a
		}(), q(5, 0), true},
		{"repeat: never fired", func() domain.Alert { a := al(domain.AlertPriceAbove, 1); a.Repeat = true; return a }(), q(5, 0), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Evaluate(tt.alert, tt.quote, now))
		})
	}
}

type fixedClock struct{ t atomic.Pointer[time.Time] }

func (c *fixedClock) Now() time.Time  { return *c.t.Load() }
func (c *fixedClock) Set(t time.Time) { c.t.Store(&t) }

type countingNotifier struct{ n atomic.Int32 }

func (c *countingNotifier) Notify(context.Context, Notification) error {
	c.n.Add(1)
	return nil
}

func setup(t *testing.T, at time.Time) (*Engine, *store.Store, *mock.Provider, *fixedClock, *countingNotifier) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	clk := &fixedClock{}
	clk.Set(at)
	st.SetClock(clk.Now)
	mk := mock.New(mock.WithClock(clk.Now))
	notif := &countingNotifier{}
	e := &Engine{
		Store: st, Market: mk, Notifier: notif, Hub: NewHub(), Clock: clk,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return e, st, mk, clk, notif
}

var (
	friday   = time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC) // market open
	saturday = time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC) // closed
)

func TestEngineOneTimeAlertFiresOnce(t *testing.T) {
	ctx := context.Background()
	e, st, mk, clk, notif := setup(t, friday)
	a, err := st.CreateAlert(ctx, domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceAbove, Threshold: 300, Cooldown: time.Hour})
	require.NoError(t, err)

	assert.Empty(t, e.Cycle(ctx, false), "price below threshold")
	require.NoError(t, mk.SetPrice("AAPL", 301))
	sub, unsub := e.Hub.Subscribe()
	defer unsub()

	fired := e.Cycle(ctx, false)
	require.Len(t, fired, 1)
	assert.Equal(t, a.ID, fired[0].AlertID)
	assert.Contains(t, fired[0].Message, "above your $300.00 alert")
	assert.Equal(t, int32(1), notif.n.Load())
	select {
	case n := <-sub:
		assert.Equal(t, "AAPL", n.Symbol)
	default:
		t.Fatal("hub subscriber got nothing")
	}

	clk.Set(friday.Add(2 * time.Hour))
	assert.Empty(t, e.Cycle(ctx, false), "one-time alert never fires again")
	got, _ := st.GetAlert(ctx, a.ID)
	assert.Equal(t, domain.AlertTriggered, got.Status)
}

func TestEngineRepeatRespectsCooldown(t *testing.T) {
	ctx := context.Background()
	e, st, mk, clk, _ := setup(t, friday)
	_, err := st.CreateAlert(ctx, domain.Alert{Symbol: "NVDA", Kind: domain.AlertPriceBelow, Threshold: 50, Repeat: true, Cooldown: 30 * time.Minute})
	require.NoError(t, err)
	require.NoError(t, mk.SetPrice("NVDA", 40))

	require.Len(t, e.Cycle(ctx, false), 1)
	clk.Set(friday.Add(10 * time.Minute))
	assert.Empty(t, e.Cycle(ctx, false), "within cooldown")
	clk.Set(friday.Add(31 * time.Minute))
	assert.Len(t, e.Cycle(ctx, false), 1, "after cooldown")
}

func TestEngineSkipsClosedMarketUnlessForcedOrAlwaysOn(t *testing.T) {
	ctx := context.Background()
	e, st, mk, _, _ := setup(t, saturday)
	_, err := st.CreateAlert(ctx, domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceAbove, Threshold: 1, Cooldown: time.Hour})
	require.NoError(t, err)
	require.NoError(t, mk.SetPrice("AAPL", 10))

	assert.Empty(t, e.Cycle(ctx, false), "market closed")
	assert.Len(t, e.Cycle(ctx, true), 1, "forced cycle (demo nudge)")

	e2, st2, mk2, _, _ := setup(t, saturday)
	e2.AlwaysOn = true
	_, _ = st2.CreateAlert(ctx, domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceAbove, Threshold: 1, Cooldown: time.Hour})
	require.NoError(t, mk2.SetPrice("AAPL", 10))
	assert.Len(t, e2.Cycle(ctx, false), 1)
}

func TestEngineRotatesSymbols(t *testing.T) {
	e := &Engine{MaxSymbolsPerCycle: 2}
	e.init()
	syms := []string{"A", "B", "C", "D", "E"}
	assert.Equal(t, []string{"A", "B"}, e.rotate(syms))
	assert.Equal(t, []string{"C", "D"}, e.rotate(syms))
	assert.Equal(t, []string{"E", "A"}, e.rotate(syms))
	assert.Equal(t, []string{"X"}, e.rotate([]string{"X"}))
}

func TestEngineRunAndTrigger(t *testing.T) {
	e, st, mk, _, notif := setup(t, saturday)
	e.Interval = time.Hour
	_, _ = st.CreateAlert(context.Background(), domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceAbove, Threshold: 1, Cooldown: time.Hour})
	require.NoError(t, mk.SetPrice("AAPL", 10))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	e.Trigger()
	require.Eventually(t, func() bool { return notif.n.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	cancel()
	<-done
}

func TestDiscordNotifier(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	d := NewDiscordWebhookNotifier(srv.URL + "/api/webhooks/123/secret-token")
	require.NoError(t, d.Notify(context.Background(), Notification{Message: "AAPL is at $301.00"}))
	assert.Equal(t, "🔔 AAPL is at $301.00", got["content"])
	assert.Equal(t, map[string]any{"parse": []any{}}, got["allowed_mentions"])

	bad := NewDiscordWebhookNotifier("http://127.0.0.1:1/api/webhooks/123/secret-token")
	err := bad.Notify(context.Background(), Notification{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-token", "webhook URL never appears in errors")

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) }))
	defer srv2.Close()
	assert.ErrorContains(t, NewDiscordWebhookNotifier(srv2.URL).Notify(context.Background(), Notification{}), "status 400")
}

func TestHubUnsubscribe(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	unsub()
	unsub() // idempotent
	h.Broadcast(Notification{})
	_, open := <-ch
	assert.False(t, open)
}
