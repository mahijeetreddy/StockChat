package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

// memStore is an in-memory WatchlistStore + AlertStore.
type memStore struct {
	watch  []string
	alerts map[int64]domain.Alert
	nextID int64
}

func newMemStore() *memStore { return &memStore{alerts: map[int64]domain.Alert{}} }

func (m *memStore) WatchlistSymbols(context.Context) ([]string, error) {
	return append([]string(nil), m.watch...), nil
}
func (m *memStore) AddToWatchlist(_ context.Context, s string) (bool, error) {
	m.watch = append(m.watch, s)
	return true, nil
}
func (m *memStore) RemoveFromWatchlist(_ context.Context, s string) (bool, error) {
	for i, x := range m.watch {
		if x == s {
			m.watch = append(m.watch[:i], m.watch[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}
func (m *memStore) CreateAlert(_ context.Context, a domain.Alert) (domain.Alert, error) {
	m.nextID++
	a.ID, a.Status = m.nextID, domain.AlertActive
	m.alerts[a.ID] = a
	return a, nil
}
func (m *memStore) GetAlert(_ context.Context, id int64) (domain.Alert, error) {
	a, ok := m.alerts[id]
	if !ok {
		return domain.Alert{}, errors.New("not found")
	}
	return a, nil
}
func (m *memStore) ListAlerts(_ context.Context, activeOnly bool) ([]domain.Alert, error) {
	var out []domain.Alert
	for _, a := range m.alerts {
		if a.Status == domain.AlertDeleted || (activeOnly && a.Status != domain.AlertActive) {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (m *memStore) CountActiveAlerts(ctx context.Context) (int, error) {
	a, _ := m.ListAlerts(ctx, true)
	return len(a), nil
}
func (m *memStore) DeleteAlert(_ context.Context, id int64) error {
	a := m.alerts[id]
	a.Status = domain.AlertDeleted
	m.alerts[id] = a
	return nil
}

func TestCreateAlertValidation(t *testing.T) {
	tool := &CreateAlert{Store: newMemStore(), Market: mockAt(friday)}
	bad := []string{
		`{"symbol":"AAPL","kind":"price_sideways","threshold":1}`,
		`{"symbol":"AAPL","kind":"price_above","threshold":-5}`,
		`{"symbol":"AAPL","kind":"percent_change_day","threshold":150}`,
		`{"symbol":"AAPL","kind":"price_above","threshold":250,"cooldown_minutes":0}`,
		`{"symbol":"AAPL","kind":"price_above","threshold":250,"cooldown_minutes":5000}`,
		`{"symbol":"AAPL","kind":"price_above","threshold":250,"webhook":"http://x"}`,
		`{"symbol":"ZZZZ","kind":"price_above","threshold":250}`,
	}
	for _, in := range bad {
		_, _, err := tool.Prepare(context.Background(), json.RawMessage(in))
		var ie *InputError
		assert.ErrorAs(t, err, &ie, in)
	}
}

func TestCreateAlertPrepareThenExecute(t *testing.T) {
	st := newMemStore()
	tool := &CreateAlert{Store: st, Market: mockAt(friday)}
	summary, norm, err := tool.Prepare(context.Background(), json.RawMessage(`{"symbol":"nvda","kind":"percent_change_day","threshold":5,"repeat":true,"cooldown_minutes":30}`))
	require.NoError(t, err)
	assert.Contains(t, summary, "NVDA moves 5.00% or more today")
	assert.Contains(t, summary, "repeating every 30 min")
	assert.Empty(t, st.alerts, "Prepare never writes")

	res, err := tool.Execute(context.Background(), norm)
	require.NoError(t, err)
	assert.Equal(t, "Alert #1 created: NVDA moves 5.00% or more today (up or down), repeating every 30 min at most", res.ForModel)
	assert.Equal(t, 30*time.Minute, st.alerts[1].Cooldown)
}

func TestWatchlistChange(t *testing.T) {
	st := newMemStore()
	add := &WatchlistChange{Store: st, Market: mockAt(friday)}
	remove := &WatchlistChange{Store: st, Market: mockAt(friday), Remove: true}
	ctx := context.Background()

	summary, norm, err := add.Prepare(ctx, json.RawMessage(`{"symbol":"tsla"}`))
	require.NoError(t, err)
	assert.Equal(t, "Add TSLA to your watchlist", summary)
	_, err = add.Execute(ctx, norm)
	require.NoError(t, err)

	_, _, err = add.Prepare(ctx, json.RawMessage(`{"symbol":"TSLA"}`))
	assert.ErrorContains(t, err, "already on the watchlist")
	_, _, err = add.Prepare(ctx, json.RawMessage(`{"symbol":"ZZZZ"}`))
	assert.ErrorContains(t, err, "valid US ticker")
	_, _, err = remove.Prepare(ctx, json.RawMessage(`{"symbol":"NVDA"}`))
	assert.ErrorContains(t, err, "not on the watchlist")

	_, norm, err = remove.Prepare(ctx, json.RawMessage(`{"symbol":"TSLA"}`))
	require.NoError(t, err)
	_, err = remove.Execute(ctx, norm)
	require.NoError(t, err)
	assert.Empty(t, st.watch)
	assert.Equal(t, "watchlist_remove", remove.Spec().Name)
}

func TestDeleteAlertTool(t *testing.T) {
	st := newMemStore()
	a, _ := st.CreateAlert(context.Background(), domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceBelow, Threshold: 100, Cooldown: time.Hour})
	tool := &DeleteAlertTool{Store: st}
	summary, norm, err := tool.Prepare(context.Background(), json.RawMessage(`{"alert_id":1}`))
	require.NoError(t, err)
	assert.Equal(t, "Delete alert #1 (AAPL falls below $100.00, one time)", summary)
	_, err = tool.Execute(context.Background(), norm)
	require.NoError(t, err)
	assert.Equal(t, domain.AlertDeleted, st.alerts[a.ID].Status)
	_, _, err = tool.Prepare(context.Background(), json.RawMessage(`{"alert_id":1}`))
	assert.ErrorContains(t, err, "doesn't exist")
	_, _, err = tool.Prepare(context.Background(), json.RawMessage(`{"alert_id":0}`))
	assert.Error(t, err)
}

func TestListTools(t *testing.T) {
	st := newMemStore()
	st.watch = []string{"AAPL", "ZZZZ"}
	_, _ = st.CreateAlert(context.Background(), domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceAbove, Threshold: 250, Cooldown: time.Hour})

	wl := &ListWatchlist{Store: st, Market: mockAt(friday), Now: clock(friday)}
	res, err := wl.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	var ql QuoteList
	require.NoError(t, json.Unmarshal(res.UI.Data, &ql))
	assert.Len(t, ql.Quotes, 1)
	assert.Equal(t, []string{"ZZZZ"}, ql.Missing)

	la := &ListAlerts{Store: st}
	res, err = la.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, res.ForModel, `"id":1`)
	var al AlertListData
	require.NoError(t, json.Unmarshal(res.UI.Data, &al))
	require.Len(t, al.Alerts, 1)
	assert.Equal(t, 60, al.Alerts[0].CooldownMinutes)

	_, err = la.Execute(context.Background(), json.RawMessage(`{"x":1}`))
	assert.Error(t, err)
}
