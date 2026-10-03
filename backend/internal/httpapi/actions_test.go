package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm/fake"
)

// chatOnce posts a message and returns the parsed events.
func chatOnce(t *testing.T, h *harness, body string) []sseEvent {
	t.Helper()
	resp := postChat(t, h, body, nil)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	evs, _ := readSSE(t, resp.Body, nil)
	return evs
}

func findEvent(t *testing.T, evs []sseEvent, name string) sseEvent {
	t.Helper()
	for _, e := range evs {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("event %q not found in %v", name, names(evs))
	return sseEvent{}
}

func post(t *testing.T, h *harness, path string) (int, actionResponse) {
	t.Helper()
	resp, err := http.Post(h.srv.URL+path, "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	var body actionResponse
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, body
}

func getJSON(t *testing.T, h *harness, path string, dst any) {
	t.Helper()
	resp, err := http.Get(h.srv.URL + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(dst))
}

func alertScript() *fake.Client {
	return fake.NewFunc(func(turn int, req llm.Request) fake.Response {
		last := req.Messages[len(req.Messages)-1]
		if last.HasToolResults() {
			return fake.Response{Text: []string{"It's waiting for your confirmation."}}
		}
		return fake.Response{ToolCalls: []fake.ToolCall{{Name: "create_alert", Input: map[string]any{"symbol": "aapl", "kind": "price_above", "threshold": 250}}}}
	})
}

func TestConfirmFlowCreatesAlertOnlyAfterConfirm(t *testing.T) {
	h := newHarness(t, alertScript(), Options{})
	evs := chatOnce(t, h, `{"message":"Alert me if AAPL goes above 250"}`)

	var confirm agent.ConfirmData
	require.NoError(t, json.Unmarshal(findEvent(t, evs, "confirmation_required").Data, &confirm))
	assert.Equal(t, "create_alert", confirm.Tool)
	assert.Contains(t, confirm.Summary, "AAPL rises above $250.00")
	assert.Contains(t, confirm.Summary, "one time")
	assert.JSONEq(t, `{"symbol":"AAPL","kind":"price_above","threshold":250,"repeat":false,"cooldown_minutes":60}`, string(confirm.Input))

	// Nothing is written before Confirm.
	alerts, err := h.store.ListAlerts(context.Background(), false)
	require.NoError(t, err)
	assert.Empty(t, alerts, "LLM tool call must not create the alert")

	code, res := post(t, h, "/api/actions/"+confirm.ActionID+"/confirm")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, domain.ActionDone, res.Status)
	assert.Contains(t, res.Result, "Alert #1 created")

	var list struct {
		Alerts []map[string]any `json:"alerts"`
	}
	getJSON(t, h, "/api/alerts", &list)
	require.Len(t, list.Alerts, 1)
	assert.Equal(t, "AAPL", list.Alerts[0]["symbol"])
	assert.Equal(t, 60.0, list.Alerts[0]["cooldown_minutes"])

	// Double confirm is rejected and reports the real status.
	code, res = post(t, h, "/api/actions/"+confirm.ActionID+"/confirm")
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, domain.ActionDone, res.Status)
	alerts, _ = h.store.ListAlerts(context.Background(), false)
	assert.Len(t, alerts, 1, "no duplicate alert")

	// The conversation records the real state for later turns, and a reload
	// shows the confirm card as done.
	var conv struct {
		Messages []publicMessage `json:"messages"`
	}
	convoID := strings.Trim(string(findEvent(t, evs, "conversation").Data), "")
	var cd agent.ConversationData
	require.NoError(t, json.Unmarshal([]byte(convoID), &cd))
	getJSON(t, h, "/api/conversations/"+cd.ConversationID, &conv)
	last := conv.Messages[len(conv.Messages)-1]
	require.Equal(t, "user", last.Role)
	assert.Contains(t, last.Blocks[0].Text, "[system note: the user confirmed action "+confirm.ActionID)
	assert.Contains(t, last.Blocks[0].Text, "Alert #1 created")
	var card *agent.ConfirmData
	for _, m := range conv.Messages {
		for _, u := range m.UI {
			if u.Confirm != nil {
				card = u.Confirm
			}
		}
	}
	require.NotNil(t, card)
	assert.Equal(t, domain.ActionDone, card.Status)
}

func TestCancelAction(t *testing.T) {
	h := newHarness(t, alertScript(), Options{})
	evs := chatOnce(t, h, `{"message":"alert"}`)
	var confirm agent.ConfirmData
	require.NoError(t, json.Unmarshal(findEvent(t, evs, "confirmation_required").Data, &confirm))

	code, res := post(t, h, "/api/actions/"+confirm.ActionID+"/cancel")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, domain.ActionCancelled, res.Status)

	code, _ = post(t, h, "/api/actions/"+confirm.ActionID+"/confirm")
	assert.Equal(t, http.StatusConflict, code, "cancelled actions can't be confirmed")
	alerts, _ := h.store.ListAlerts(context.Background(), false)
	assert.Empty(t, alerts)
}

func TestExpiredActionRejected(t *testing.T) {
	h := newHarness(t, alertScript(), Options{})
	evs := chatOnce(t, h, `{"message":"alert"}`)
	var confirm agent.ConfirmData
	require.NoError(t, json.Unmarshal(findEvent(t, evs, "confirmation_required").Data, &confirm))

	h.store.SetClock(func() time.Time { return friday.Add(16 * time.Minute) })
	code, res := post(t, h, "/api/actions/"+confirm.ActionID+"/confirm")
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, domain.ActionExpired, res.Status)
	assert.Contains(t, res.Error, "expired")
	alerts, _ := h.store.ListAlerts(context.Background(), false)
	assert.Empty(t, alerts)
}

func TestUnknownActionIs404(t *testing.T) {
	h := newHarness(t, fake.New(), Options{})
	code, _ := post(t, h, "/api/actions/act_missing/confirm")
	assert.Equal(t, http.StatusNotFound, code)
	code, _ = post(t, h, "/api/actions/act_missing/cancel")
	assert.Equal(t, http.StatusNotFound, code)
}

func TestInvalidMutationIsRejectedBeforeStaging(t *testing.T) {
	client := fake.New(
		fake.Response{ToolCalls: []fake.ToolCall{{Name: "create_alert", Input: map[string]any{"symbol": "ZZZZ", "kind": "price_above", "threshold": 1}}}},
		fake.Response{Text: []string{"That ticker doesn't exist."}},
	)
	h := newHarness(t, client, Options{})
	evs := chatOnce(t, h, `{"message":"alert on ZZZZ"}`)
	assert.NotContains(t, names(evs), "confirmation_required")
	var res agent.ToolResultData
	require.NoError(t, json.Unmarshal(findEvent(t, evs, "tool_result").Data, &res))
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "valid US ticker")
}

func TestWatchlistAddViaConfirmAndRESTEndpoints(t *testing.T) {
	client := fake.New(
		fake.Response{ToolCalls: []fake.ToolCall{
			{Name: "watchlist_add", Input: map[string]string{"symbol": "TSLA"}},
			{Name: "watchlist_add", Input: map[string]string{"symbol": "NVDA"}},
		}},
		fake.Response{Text: []string{"Confirm to add them."}},
	)
	h := newHarness(t, client, Options{})
	evs := chatOnce(t, h, `{"message":"Add TSLA and NVDA to my watchlist"}`)
	var ids []string
	for _, e := range evs {
		if e.Name == "confirmation_required" {
			var c agent.ConfirmData
			require.NoError(t, json.Unmarshal(e.Data, &c))
			ids = append(ids, c.ActionID)
		}
	}
	require.Len(t, ids, 2)
	for _, id := range ids {
		code, _ := post(t, h, "/api/actions/"+id+"/confirm")
		require.Equal(t, http.StatusOK, code)
	}
	var wl struct {
		Symbols []string         `json:"symbols"`
		Quotes  []map[string]any `json:"quotes"`
	}
	getJSON(t, h, "/api/watchlist", &wl)
	assert.ElementsMatch(t, []string{"TSLA", "NVDA"}, wl.Symbols)
	assert.Len(t, wl.Quotes, 2)
}

func TestDeleteAlertEndpoint(t *testing.T) {
	h := newHarness(t, fake.New(), Options{})
	a, err := h.store.CreateAlert(context.Background(), domain.Alert{Symbol: "AAPL", Kind: domain.AlertPriceBelow, Threshold: 100, Cooldown: time.Hour})
	require.NoError(t, err)
	req, _ := http.NewRequest(http.MethodDelete, h.srv.URL+"/api/alerts/1", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	got, _ := h.store.GetAlert(context.Background(), a.ID)
	assert.Equal(t, domain.AlertDeleted, got.Status)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestNotesQueuedWhileReplyInProgress(t *testing.T) {
	h := newHarness(t, fake.New(), Options{})
	conv, err := h.store.CreateConversation(context.Background(), "c_q")
	require.NoError(t, err)
	require.True(t, h.api.tryLock(conv.ID))
	h.api.addNote(conv.ID, "[system note: queued]")
	msgs, _ := h.store.Messages(context.Background(), conv.ID)
	assert.Empty(t, msgs, "note waits while a reply streams")
	h.api.unlock(conv.ID)
	msgs, _ = h.store.Messages(context.Background(), conv.ID)
	require.Len(t, msgs, 1)
	assert.Equal(t, "[system note: queued]", msgs[0].Blocks[0].Text)
}
