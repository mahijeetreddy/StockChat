package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

func TestWatchlist(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	added, err := s.AddToWatchlist(ctx, "TSLA")
	require.NoError(t, err)
	assert.True(t, added)
	added, _ = s.AddToWatchlist(ctx, "TSLA")
	assert.False(t, added, "duplicate ignored")
	_, _ = s.AddToWatchlist(ctx, "NVDA")
	syms, err := s.WatchlistSymbols(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"TSLA", "NVDA"}, syms)
	removed, _ := s.RemoveFromWatchlist(ctx, "TSLA")
	assert.True(t, removed)
	removed, _ = s.RemoveFromWatchlist(ctx, "TSLA")
	assert.False(t, removed)
}

func TestAlertsLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	once, err := s.CreateAlert(ctx, domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceAbove, Threshold: 250, Cooldown: time.Hour})
	require.NoError(t, err)
	rep, err := s.CreateAlert(ctx, domain.Alert{Symbol: "NVDA", Kind: domain.AlertPercentChangeDay, Threshold: 5, Repeat: true, Cooldown: 30 * time.Minute})
	require.NoError(t, err)

	got, err := s.GetAlert(ctx, rep.ID)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, got.Cooldown)
	assert.True(t, got.Repeat)
	assert.Nil(t, got.LastFired)

	n, _ := s.CountActiveAlerts(ctx)
	assert.Equal(t, 2, n)

	at := time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	evID, fired, err := s.RecordAlertFire(ctx, once, 251, at)
	require.NoError(t, err)
	assert.True(t, fired)
	assert.Positive(t, evID)
	require.NoError(t, s.MarkEventDelivered(ctx, evID))

	got, _ = s.GetAlert(ctx, once.ID)
	assert.Equal(t, domain.AlertTriggered, got.Status, "one-time alerts become triggered")
	require.NotNil(t, got.LastFired)
	assert.True(t, at.Equal(*got.LastFired))

	_, fired, err = s.RecordAlertFire(ctx, once, 252, at.Add(time.Minute))
	require.NoError(t, err)
	assert.False(t, fired, "a triggered alert doesn't fire again")

	_, fired, _ = s.RecordAlertFire(ctx, rep, 10, at)
	assert.True(t, fired)
	got, _ = s.GetAlert(ctx, rep.ID)
	assert.Equal(t, domain.AlertActive, got.Status, "repeating alerts stay active")

	active, _ := s.ListAlerts(ctx, true)
	require.Len(t, active, 1)
	all, _ := s.ListAlerts(ctx, false)
	require.Len(t, all, 2)
	assert.Equal(t, rep.ID, all[0].ID, "active first")

	require.NoError(t, s.DeleteAlert(ctx, rep.ID))
	assert.ErrorIs(t, s.DeleteAlert(ctx, rep.ID), ErrNotFound)
	_, fired, _ = s.RecordAlertFire(ctx, rep, 10, at.Add(2*time.Hour))
	assert.False(t, fired, "deleted alerts never fire")
}

func TestPendingActionTransitions(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	_, err := s.CreateConversation(ctx, "c1")
	require.NoError(t, err)
	mk := func(id string) {
		require.NoError(t, s.CreatePendingAction(ctx, domain.PendingAction{
			ID: id, ConversationID: "c1", ToolName: "watchlist_add", Input: []byte(`{"symbol":"AAPL"}`),
			Summary: "Add AAPL", CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute),
		}))
	}
	mk("a1")
	mk("a2")
	mk("a3")

	a, err := s.ClaimPendingAction(ctx, "a1")
	require.NoError(t, err)
	assert.Equal(t, ActionExecuting, a.Status)
	assert.JSONEq(t, `{"symbol":"AAPL"}`, string(a.Input))
	_, err = s.ClaimPendingAction(ctx, "a1")
	assert.ErrorIs(t, err, ErrConflict, "claimed exactly once")
	require.NoError(t, s.FinishPendingAction(ctx, "a1", domain.ActionDone, "added"))
	a, _ = s.GetPendingAction(ctx, "a1")
	assert.Equal(t, "added", a.Result)

	_, err = s.CancelPendingAction(ctx, "a2")
	require.NoError(t, err)
	_, err = s.ClaimPendingAction(ctx, "a2")
	assert.ErrorIs(t, err, ErrConflict)

	now = now.Add(20 * time.Minute)
	a, err = s.ClaimPendingAction(ctx, "a3")
	assert.ErrorIs(t, err, ErrConflict)
	assert.Equal(t, domain.ActionExpired, a.Status)

	_, err = s.ClaimPendingAction(ctx, "missing")
	assert.ErrorIs(t, err, ErrNotFound)

	mk("a4")
	now = now.Add(time.Hour)
	n, err := s.ExpirePendingActions(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}
